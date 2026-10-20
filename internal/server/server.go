package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"qdake/reportviewer/internal/fr"
	webui "qdake/reportviewer/web"
)

// Server exposes the reportlets tree, the render API and the embedded UI.
type Server struct {
	Webroot    string
	Reportlets string

	dr       *fr.DataResolver
	index    *templateIndex
	mux      *http.ServeMux
	fvsMu    sync.Mutex
	fvsCache map[string]*fvsEntry

	// metrics counts requests for the health endpoint.
	metrics *Metrics
	// auth guards the endpoints when credentials are configured.
	auth *Auth
	// requestTimeout bounds a single render; zero means no limit.
	requestTimeout time.Duration
}

type fvsEntry struct {
	mu  sync.Mutex
	mod time.Time
	tpl *fr.FVSTemplate
}

func New(webroot, reportlets string) (*Server, error) {
	absRoot, err := filepath.Abs(webroot)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(absRoot); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("FineReport webroot 不存在: %s", absRoot)
	}
	if reportlets == "" {
		reportlets = filepath.Join(absRoot, "WEB-INF", "reportlets")
	}
	absRep, err := filepath.Abs(reportlets)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(absRep); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("reportlets 目录不存在: %s", absRep)
	}
	s := &Server{
		Webroot:        absRoot,
		Reportlets:     absRep,
		dr:             fr.NewDataResolver(absRoot),
		fvsCache:       map[string]*fvsEntry{},
		metrics:        NewMetrics(),
		auth:           NewAuth(),
		requestTimeout: parseDurationEnv("RV_TIMEOUT", 60*time.Second),
	}
	s.index = newTemplateIndex(absRep)
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/api/templates", s.handleTemplates)
	s.mux.HandleFunc("/api/preview", s.handlePreview)
	s.mux.HandleFunc("/api/fvs/asset", s.handleAsset)
	s.mux.HandleFunc("/api/connections", s.handleConnections)
	s.mux.HandleFunc("/api/health", s.healthHandler)
	s.mux.Handle("/", http.FileServer(http.FS(webui.FS)))
	return s, nil
}

func (s *Server) Close() { s.dr.Close() }

// Handler returns the HTTP handler with the middleware chain applied:
// request metrics and timeout first, then authentication, then the routes.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	if s.auth.Enabled() {
		h = s.auth.Middleware(h)
	}
	return s.metrics.Middleware(h, s.requestTimeout)
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.index.list()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"templates": list,
		"count":     len(list),
	})
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	abs, rel, err := s.resolve(q.Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	params := map[string]string{}
	for k, vals := range q {
		if k == "path" || k == "_" || len(vals) == 0 {
			continue
		}
		params[k] = vals[0]
	}
	start := time.Now()
	p, err := s.render(abs, rel, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p.Path = rel
	w.Header().Set("X-Render-Ms", fmt.Sprintf("%d", time.Since(start).Milliseconds()))
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) render(abs, rel string, params map[string]string) (*fr.Preview, error) {
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".cpt":
		tpl, err := fr.ParseCPTFile(abs)
		if err != nil {
			return nil, err
		}
		return tpl.Render(s.dr, params), nil
	case ".fvs":
		e, err := s.getFVS(abs)
		if err != nil {
			return nil, err
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.tpl.Render(s.dr, params), nil
	default:
		return nil, fmt.Errorf("仅支持 .cpt 和 .fvs 文件")
	}
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	abs, _, err := s.resolve(q.Get("tpl"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !strings.EqualFold(filepath.Ext(abs), ".fvs") {
		writeError(w, http.StatusBadRequest, "资源仅适用于 .fvs 模板")
		return
	}
	id := q.Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "缺少资源 id")
		return
	}
	e, err := s.getFVS(abs)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	data, mime, err := e.tpl.ReadAsset(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}

func (s *Server) getFVS(abs string) (*fvsEntry, error) {
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	mod := st.ModTime()
	s.fvsMu.Lock()
	if e := s.fvsCache[abs]; e != nil && e.mod.Equal(mod) {
		s.fvsMu.Unlock()
		return e, nil
	}
	s.fvsMu.Unlock()

	tpl, err := fr.ParseFVSFile(abs)
	if err != nil {
		return nil, err
	}
	e := &fvsEntry{mod: mod, tpl: tpl}
	s.fvsMu.Lock()
	s.fvsCache[abs] = e
	s.fvsMu.Unlock()
	return e, nil
}

// resolve accepts a path relative to reportlets (or an absolute path inside it)
// and rejects anything outside the reportlets tree.
func (s *Server) resolve(p string) (abs, rel string, err error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", fmt.Errorf("缺少模板路径")
	}
	p = filepath.FromSlash(p)
	var cand string
	if filepath.IsAbs(p) {
		cand = p
	} else {
		cand = filepath.Join(s.Reportlets, p)
	}
	cand, err = filepath.Abs(filepath.Clean(cand))
	if err != nil {
		return "", "", err
	}
	r, err := filepath.Rel(s.Reportlets, cand)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", "", fmt.Errorf("模板路径超出 reportlets 目录")
	}
	st, err := os.Stat(cand)
	if err != nil || st.IsDir() {
		return "", "", fmt.Errorf("模板不存在: %s", filepath.ToSlash(r))
	}
	ext := strings.ToLower(filepath.Ext(cand))
	if ext != ".cpt" && ext != ".fvs" {
		return "", "", fmt.Errorf("仅支持 .cpt 和 .fvs 文件")
	}
	return cand, filepath.ToSlash(r), nil
}

// handleConnections reports every datasource declared in the FineReport
// installation plus whether its password is available. Passwords themselves
// are never sent to the browser; only the env var name to set.
func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	list := s.dr.Connections().List()
	ready := 0
	for _, c := range list {
		if !c.NeedsPassword || c.Resolved {
			ready++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": list,
		"total":       len(list),
		"ready":       ready,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ---------- template index ----------

type templateIndex struct {
	root  string
	mu    sync.Mutex
	last  time.Time
	listv []fr.TemplateInfo
}

func newTemplateIndex(root string) *templateIndex {
	return &templateIndex{root: root}
}

func (t *templateIndex) list() ([]fr.TemplateInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.last.IsZero() && time.Since(t.last) < 30*time.Second {
		return t.listv, nil
	}
	out, err := scanTemplates(t.root)
	if err != nil {
		return nil, err
	}
	t.listv = out
	t.last = time.Now()
	return out, nil
}

func scanTemplates(root string) ([]fr.TemplateInfo, error) {
	var out []fr.TemplateInfo
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".cpt" && ext != ".fvs" {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		out = append(out, fr.TemplateInfo{
			Path: relSlash,
			Name: strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())),
			Kind: strings.TrimPrefix(ext, "."),
			Dir:  dir,
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := strings.ToLower(out[i].Dir), strings.ToLower(out[j].Dir)
		if di != dj {
			return di < dj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}
