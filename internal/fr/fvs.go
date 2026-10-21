package fr

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FVSTemplate is a parsed FineVis dashboard (.fvs), which is a ZIP archive
// containing editor.tpl (canvas JSON + datasets), chart XML and report blocks.
type FVSTemplate struct {
	Path      string
	Name      string
	Width     float64
	Height    float64
	Stories   []*FVSStory
	Datasets  map[string]*Dataset
	Resources map[string]FVSResource
	Params    []ParamDef
	Warnings  []string

	charts    map[string][]byte
	ecs       map[string][]byte
	byResName map[string]string
}

type FVSStory struct {
	Name       string
	Background map[string]any
	Border     map[string]any
	Hidden     bool
	Widgets    []*FVSWidget
}

type FVSWidget struct {
	ID       string
	Type     string
	Parent   string
	Name     string
	Children []string
	Hide     bool
	X, Y     float64
	W, H     float64
	Props    map[string]any
	Order    int
}

type FVSResource struct {
	ID   string
	Name string
	Type string
}

type fvsStore struct {
	Name       string                    `json:"name"`
	Width      float64                   `json:"width"`
	Height     float64                   `json:"height"`
	Stories    []fvsStoryRaw             `json:"stories"`
	Resources  map[string]fvsResourceRaw `json:"resources"`
	Parameters json.RawMessage           `json:"parameterConfigs"`
}

type fvsStoryRaw struct {
	Name            string          `json:"name"`
	Background      json.RawMessage `json:"background"`
	Border          json.RawMessage `json:"border"`
	IsPreviewHidden bool            `json:"isPreviewHidden"`
	Widgets         []fvsWidgetRaw  `json:"widgets"`
}

type fvsWidgetRaw struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Parent   string         `json:"parent"`
	Name     string         `json:"name"`
	Children []string       `json:"children"`
	Hide     bool           `json:"hide"`
	Props    map[string]any `json:"props"`
	States   struct {
		Width  float64   `json:"width"`
		Height float64   `json:"height"`
		Matrix []float64 `json:"matrix"`
		Top    float64   `json:"top"`
		Left   float64   `json:"left"`
	} `json:"states"`
}

type fvsResourceRaw struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ParseFVSFile reads a FineVis archive and extracts the canvas and datasets.
func ParseFVSFile(path string) (*FVSTemplate, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("打开 FVS 文件失败: %w", err)
	}
	defer zr.Close()
	t := &FVSTemplate{
		Path:      path,
		Name:      strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Datasets:  map[string]*Dataset{},
		Resources: map[string]FVSResource{},
		charts:    map[string][]byte{},
		ecs:       map[string][]byte{},
		byResName: map[string]string{},
	}
	var tplData []byte
	var assetFiles []string
	for _, f := range zr.File {
		name := f.Name
		switch {
		case name == "editor.tpl":
			b, err := readZipFile(f)
			if err != nil {
				return nil, fmt.Errorf("读取 editor.tpl 失败: %w", err)
			}
			tplData = b
		case strings.HasSuffix(strings.ToLower(name), ".chart"):
			if b, err := readZipFile(f); err == nil {
				t.charts[name] = b
			}
		case strings.HasSuffix(strings.ToLower(name), ".ec"):
			if b, err := readZipFile(f); err == nil {
				t.ecs[name] = b
			}
		case isAssetFile(name):
			assetFiles = append(assetFiles, name)
		}
	}
	if tplData == nil {
		return nil, fmt.Errorf("不是有效的 FVS 文件（缺少 editor.tpl）")
	}
	root, err := ParseXML(tplData)
	if err != nil {
		return nil, fmt.Errorf("解析 editor.tpl 失败: %w", err)
	}
	rawStore := root.Attr("store")
	if rawStore == "" {
		if n := root.Find("DuchampTemplateAttr"); n != nil {
			rawStore = n.Attr("store")
		}
	}
	if rawStore == "" {
		for _, n := range root.FindAll("DuchampTemplateAttr") {
			if rawStore = n.Attr("store"); rawStore != "" {
				break
			}
		}
	}
	if rawStore == "" {
		return nil, fmt.Errorf("editor.tpl 中找不到画布数据")
	}
	var store fvsStore
	if err := json.Unmarshal([]byte(rawStore), &store); err != nil {
		if u := html.UnescapeString(rawStore); u != rawStore {
			if err2 := json.Unmarshal([]byte(u), &store); err2 != nil {
				return nil, fmt.Errorf("解析画布 JSON 失败: %w", err2)
			}
		} else {
			return nil, fmt.Errorf("解析画布 JSON 失败: %w", err)
		}
	}
	t.Width = store.Width
	t.Height = store.Height
	if store.Name != "" {
		t.Name = store.Name
	}
	t.Datasets = ParseDatasets(root)
	for _, ds := range t.Datasets {
		t.Warnings = append(t.Warnings, ds.Warnings...)
	}
	for id, r := range store.Resources {
		t.Resources[id] = FVSResource{ID: id, Name: r.Name, Type: r.Type}
		t.byResName[strings.TrimPrefix(r.Name, "/")] = id
	}
	// Some dashboards reference images only by their archive file name and do
	// not list them in the store's resource table.
	for _, name := range assetFiles {
		if _, ok := t.byResName[name]; ok {
			continue
		}
		if _, ok := t.Resources[name]; ok {
			continue
		}
		t.Resources[name] = FVSResource{ID: name, Name: name, Type: "image"}
		t.byResName[name] = name
	}
	for i, sr := range store.Stories {
		story := &FVSStory{Name: sr.Name, Hidden: sr.IsPreviewHidden}
		if story.Name == "" {
			story.Name = fmt.Sprintf("页面%d", i+1)
		}
		if len(sr.Background) > 0 {
			_ = json.Unmarshal(sr.Background, &story.Background)
		}
		if len(sr.Border) > 0 {
			_ = json.Unmarshal(sr.Border, &story.Border)
		}
		for j := range sr.Widgets {
			rw := &sr.Widgets[j]
			w := &FVSWidget{
				ID:       rw.ID,
				Type:     rw.Type,
				Parent:   rw.Parent,
				Name:     rw.Name,
				Children: rw.Children,
				Hide:     rw.Hide,
				W:        rw.States.Width,
				H:        rw.States.Height,
				Props:    rw.Props,
				Order:    j,
			}
			if w.Props == nil {
				w.Props = map[string]any{}
			}
			if len(rw.States.Matrix) >= 6 {
				w.X, w.Y = rw.States.Matrix[4], rw.States.Matrix[5]
			} else {
				w.X, w.Y = rw.States.Left, rw.States.Top
			}
			story.Widgets = append(story.Widgets, w)
		}
		t.Stories = append(t.Stories, story)
	}
	if len(t.Stories) == 0 {
		return nil, fmt.Errorf("FVS 中没有可渲染的页面")
	}
	if t.Width <= 0 {
		t.Width = 960
	}
	if t.Height <= 0 {
		t.Height = 540
	}
	return t, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 64<<20))
}

// ReadAsset returns an embedded image or font resource.
func (t *FVSTemplate) ReadAsset(resourceID string) ([]byte, string, error) {
	name := strings.TrimPrefix(resourceID, "/")
	if res, ok := t.Resources[resourceID]; ok {
		name = strings.TrimPrefix(res.Name, "/")
	}
	zr, err := zip.OpenReader(t.Path)
	if err != nil {
		return nil, "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		b, err := readZipFile(f)
		if err != nil {
			return nil, "", err
		}
		return b, mimeByExt(name), nil
	}
	return nil, "", fmt.Errorf("FVS 中缺少资源文件: %s", name)
}

func isAssetFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp", ".woff", ".woff2", ".ttf", ".mp4":
		return true
	}
	return false
}

func mimeByExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".mp4":
		return "video/mp4"
	}
	return "application/octet-stream"
}

// Render paints the canvas into absolute-positioned widgets for the browser.
func (t *FVSTemplate) Render(dr *DataResolver, params map[string]string) *Preview {
	p := &Preview{Path: t.Path, Name: t.Name, Kind: "fvs", Title: t.Name, Params: t.Params}
	base := &CPTTemplate{Path: t.Path, Name: t.Name, Datasets: t.Datasets, Params: t.Params}
	st := newRenderState(base, dr, params)
	for _, w := range t.Warnings {
		st.warn(w)
	}
	st.loadDatasets()
	for _, story := range t.Stories {
		page := FVSPage{Name: story.Name, Width: t.Width, Height: t.Height}
		t.applyStoryBackground(&page, story)
		page.Widgets = t.renderWidgets(st, story)
		p.Pages = append(p.Pages, page)
	}
	if len(p.Pages) > 0 {
		p.Width = p.Pages[0].Width
		p.Height = p.Pages[0].Height
	}
	p.Warnings = append(p.Warnings, st.warnings...)
	return p
}

func (t *FVSTemplate) applyStoryBackground(page *FVSPage, story *FVSStory) {
	// The story background paints under everything. Real dashboards either
	// set a color/image background or rely on the "internal" border texture
	// (a nine-slice JPG that carries the signature dark-blue look).
	if bg := story.Background; len(bg) > 0 {
		typ := fvsStr(bg, "type")
		switch typ {
		case "color":
			page.Background = fvsStr(bg, "value")
		case "custom", "image":
			t.applyImageBackground(page, fvsMap(bg, "value"), fvsStr(bg, "value"))
		}
	}
	if page.Background == "" && page.BackgroundImage == "" {
		if b := story.Border; len(b) > 0 {
			switch fvsStr(b, "type") {
			case "internal":
				// The nine-slice frame texture stretches well enough as a
				// full-bleed background for preview purposes.
				t.applyImageBackground(page, fvsMap(b, "value"), fvsStr(b, "value"))
			case "color":
				page.Background = fvsStr(b, "value")
			}
		}
	}
	if page.Background != "" {
		page.Theme = themeForColor(page.Background)
		return
	}
	if page.BackgroundImage != "" {
		page.Theme = "dark"
		return
	}
	// backgroundFillType "auto" (the designer default) renders the classic
	// FineReport dark-blue gradient; without it every light-on-dark widget
	// becomes white text on a white page.
	page.Background = "linear-gradient(160deg, #233F66 0%, #001223 55%, #000E1A 100%)"
	page.Theme = "dark"
}

func (t *FVSTemplate) applyImageBackground(page *FVSPage, v map[string]any, fallbackID string) {
	id := fvsStr(v, "id")
	if id == "" && len(v) == 0 {
		id = fallbackID
	}
	if id == "" {
		if rid, ok := t.byResName[fvsStr(v, "imageSource")]; ok {
			id = rid
		}
	}
	if id != "" {
		page.BackgroundImage = t.assetURL(id)
	}
	switch fvsStr(v, "backgroundFillType") {
	case "stretch":
		page.BackgroundSize = "100% 100%"
	case "repeat", "tile":
		page.BackgroundSize = "auto"
	case "cover":
		page.BackgroundSize = "cover"
	default:
		page.BackgroundSize = "100% 100%"
	}
}

// themeForColor marks a page dark when its solid background color is dark
// enough that light chart labels are required for contrast.
func themeForColor(color string) string {
	c := strings.TrimPrefix(strings.TrimSpace(color), "#")
	if len(c) == 8 {
		c = c[2:] // drop alpha
	}
	if len(c) != 6 {
		return ""
	}
	num, err := strconv.ParseUint(c, 16, 32)
	if err != nil {
		return ""
	}
	r := float64((num>>16)&0xff) / 255
	g := float64((num>>8)&0xff) / 255
	b := float64(num&0xff) / 255
	lum := 0.2126*r + 0.7152*g + 0.0722*b
	if lum < 0.45 {
		return "dark"
	}
	return ""
}

func (t *FVSTemplate) assetURL(resourceID string) string {
	return "/api/fvs/asset?tpl=" + url.QueryEscape(t.Path) + "&id=" + url.QueryEscape(resourceID)
}

type fvsPaneState struct {
	active bool
}

func (t *FVSTemplate) renderWidgets(st *renderState, story *FVSStory) []Widget {
	byID := make(map[string]*FVSWidget, len(story.Widgets))
	for _, w := range story.Widgets {
		byID[w.ID] = w
	}
	panes := map[string]fvsPaneState{}

	// Resolve slideshow tabs and which pane is currently visible.
	for _, w := range story.Widgets {
		if w.Type != "d-slideshow" {
			continue
		}
		active := fvsInt(w.Props["current"], 0)
		idx := 0
		for _, cid := range w.Children {
			c := byID[cid]
			if c == nil || c.Type != "d-tab-pane" {
				continue
			}
			panes[cid] = fvsPaneState{active: idx == active}
			idx++
		}
	}

	nearestPane := func(w *FVSWidget) string {
		seen := 0
		for p := w.Parent; p != "" && seen < 32; seen++ {
			pw := byID[p]
			if pw == nil {
				break
			}
			if pw.Type == "d-tab-pane" {
				return pw.ID
			}
			p = pw.Parent
		}
		return ""
	}

	out := make([]Widget, 0, len(story.Widgets))
	for _, w := range story.Widgets {
		if w.Hide {
			continue
		}
		switch w.Type {
		case "d-group", "d-reserve-canvas", "d-tab-pane":
			continue
		case "d-slideshow":
			ow := Widget{ID: w.ID, Type: "slideshow", X: w.X, Y: w.Y, W: w.W, H: w.H, Z: w.Order}
			ow.Tab = t.tabInfo(w, byID)
			ow.Pane = nearestPane(w)
			ow.Hidden = ow.Pane != "" && !panes[ow.Pane].active
			out = append(out, ow)
			continue
		}
		ow := Widget{ID: w.ID, Type: w.Type, Parent: w.Parent, X: w.X, Y: w.Y, W: w.W, H: w.H, Z: w.Order}
		if p := nearestPane(w); p != "" {
			ow.Pane = p
			if !panes[p].active {
				ow.Hidden = true
			}
		}
		switch {
		case w.Type == "d-text-title":
			ow.Type = "text-title"
			ow.Text, ow.HTML, ow.Style = fvsTitleText(st, w.Props)
		case w.Type == "d-text":
			ow.Type = "text"
			ow.Text, ow.HTML, ow.Style = fvsRichText(st, w.Props)
		case w.Type == "d-text-time":
			ow.Type = "text-time"
			ow.Text, ow.Style = fvsTimeText(w.Props)
		case w.Type == "d-image":
			ow.Type = "image"
			ow.ImageURL = t.imageURL(w)
		case strings.HasPrefix(w.Type, "d-chart-"):
			spec := t.chartSpec(st, w)
			if spec == nil || len(spec.Series) == 0 {
				// Custom charts without a Plot and charts whose demo data is
				// unreadable still ship a rendered preview image.
				if poster := t.posterURL(w); poster != "" {
					ow.Type = "image"
					ow.ImageURL = poster
					out = append(out, ow)
					continue
				}
			}
			if spec == nil {
				continue
			}
			ow.Type = "chart"
			ow.Chart = spec
		case w.Type == "d-workbook-x":
			ow.Type = "workbook"
			ow.Sheet = t.renderWorkbook(st, w)
		default:
			st.warn(fmt.Sprintf("FVS 暂不支持控件类型 %s", w.Type))
			continue
		}
		out = append(out, ow)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Z < out[j].Z })
	return out
}

func (t *FVSTemplate) tabInfo(w *FVSWidget, byID map[string]*FVSWidget) *TabInfo {
	tab := &TabInfo{Active: fvsInt(w.Props["current"], 0)}
	for _, cid := range w.Children {
		c := byID[cid]
		if c == nil || c.Type != "d-tab-pane" {
			continue
		}
		tab.PaneIDs = append(tab.PaneIDs, cid)
		name := fvsStr(c.Props, "tabName")
		if name == "" {
			name = c.Name
		}
		tab.Tabs = append(tab.Tabs, name)
	}
	if tab.Active < 0 || tab.Active >= len(tab.PaneIDs) {
		tab.Active = 0
	}
	if card := fvsMap(w.Props, "tabCard"); len(card) > 0 {
		tab.Position = fvsStr(card, "position")
		tab.ActiveBG = fvsStr(card, "background")
		tab.InactiveBG = fvsStr(card, "color")
		tab.FontColor = fvsStr(card, "color")
		if cs := fvsMap(card, "characterStyle"); len(cs) > 0 {
			tab.FontSize = fvsNum(cs["fontSize"], 0)
			tab.FontColor = fvsStr(cs, "color")
		}
		if sel := fvsMap(card, "selected"); len(sel) > 0 {
			tab.ActiveBG = fvsStr(sel, "background")
			if c := fvsStr(sel, "color"); c != "" {
				tab.FontColor = c
			}
		}
	}
	return tab
}

func (t *FVSTemplate) posterURL(w *FVSWidget) string {
	target := w.ID + ".png"
	if id, ok := t.byResName[target]; ok {
		if _, ok := t.Resources[id]; ok {
			return t.assetURL(id)
		}
	}
	for id, res := range t.Resources {
		name := strings.TrimPrefix(res.Name, "/")
		if name == target || strings.HasSuffix(name, "/"+target) {
			return t.assetURL(id)
		}
	}
	return ""
}

func (t *FVSTemplate) imageURL(w *FVSWidget) string {
	asset := fvsMap(w.Props, "asset")
	id := ""
	if v := fvsMap(asset, "value"); len(v) > 0 {
		id = fvsStr(v, "id")
		if id == "" {
			id = fvsStr(v, "imageSource")
		}
	}
	if id == "" {
		if v := fvsStr(asset, "value"); v != "" {
			id = v
		}
	}
	if id == "" {
		return ""
	}
	if _, ok := t.Resources[id]; !ok {
		if rid, ok := t.byResName[strings.TrimPrefix(id, "/")]; ok {
			id = rid
		}
	}
	if _, ok := t.Resources[id]; !ok {
		return ""
	}
	return t.assetURL(id)
}

func (t *FVSTemplate) chartSpec(st *renderState, w *FVSWidget) *ChartSpec {
	// FVS charts are usually stored as a separate .chart file referenced by
	// configFile, but many templates (including the built-in demos) embed the
	// full chart XML inside the widget's stringChartCollection property. When
	// no standalone file is present we fall back to that embedded XML so the
	// dashboard still paints.
	member := t.memberFor(w, t.charts, ".chart")
	var xmlBytes []byte
	if member != "" {
		xmlBytes = t.charts[member]
	} else if sc := fvsStr(w.Props, "stringChartCollection"); sc != "" {
		xmlBytes = []byte(sc)
	}
	if xmlBytes == nil {
		st.warn("FVS 图表缺少配置文件: " + w.Name)
		return nil
	}
	root, err := ParseXML(xmlBytes)
	if err != nil {
		st.warn(fmt.Sprintf("图表 %s 解析失败: %v", w.Name, err))
		return nil
	}
	spec, warns := ParseChartSpec(root, t.Datasets)
	for _, msg := range warns {
		st.warn(msg)
	}
	return spec
}

func (t *FVSTemplate) renderWorkbook(st *renderState, w *FVSWidget) *Sheet {
	member := t.memberFor(w, t.ecs, ".ec")
	if member == "" {
		st.warn("FVS 报表块缺少配置文件: " + w.Name)
		return nil
	}
	sh, err := parseECSheet(t.ecs[member], w.Name, t.Datasets)
	if err != nil {
		st.warn(fmt.Sprintf("报表块 %s 解析失败: %v", w.Name, err))
		return nil
	}
	return st.renderSheet(sh)
}

func (t *FVSTemplate) memberFor(w *FVSWidget, pool map[string][]byte, ext string) string {
	if cf := fvsStr(w.Props, "configFile"); cf != "" {
		if res, ok := t.Resources[cf]; ok {
			n := strings.TrimPrefix(res.Name, "/")
			if _, ok := pool[n]; ok {
				return n
			}
		}
	}
	if _, ok := pool[w.ID+ext]; ok {
		return w.ID + ext
	}
	for _, r := range t.Resources {
		n := strings.TrimPrefix(r.Name, "/")
		if strings.HasSuffix(n, w.ID+ext) {
			if _, ok := pool[n]; ok {
				return n
			}
		}
	}
	return ""
}

// parseECSheet turns an FVS report block (ElementCase) into a CPT sheet.
func parseECSheet(data []byte, name string, datasets map[string]*Dataset) (*SheetTemplate, error) {
	root, err := ParseXML(data)
	if err != nil {
		return nil, err
	}
	fec := root.Find("FormElementCase")
	if fec == nil {
		return nil, fmt.Errorf("缺少 FormElementCase")
	}
	styles := ParseStyleList(root)
	sh := &SheetTemplate{Name: name, Cells: map[[2]int]*CellTemplate{}}
	sh.parseSizes(fec)
	if cel := fec.Child("CellElementList"); cel != nil {
		for _, c := range cel.ChildAll("C") {
			ct := parseCell(c, styles)
			if ct == nil {
				continue
			}
			key := [2]int{ct.R, ct.C}
			sh.Cells[key] = ct
			sh.CellList = append(sh.CellList, ct)
			if ct.R+ct.RS > sh.MaxRow {
				sh.MaxRow = ct.R + ct.RS
			}
			if ct.C+ct.CS > sh.MaxCol {
				sh.MaxCol = ct.C + ct.CS
			}
		}
	}
	sh.resolveExpandParents()
	sh.fillSizes()
	sort.Slice(sh.CellList, func(i, j int) bool {
		if sh.CellList[i].R != sh.CellList[j].R {
			return sh.CellList[i].R < sh.CellList[j].R
		}
		return sh.CellList[i].C < sh.CellList[j].C
	})
	return sh, nil
}

// ---------- FVS text helpers ----------

func fvsStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return plainValue(t)
	case bool:
		return strconv.FormatBool(t)
	}
	return fmt.Sprint(v)
}

func fvsNum(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return f
		}
	}
	return def
}

func fvsInt(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return i
		}
	}
	return def
}

func fvsBool(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	case float64:
		return t != 0
	}
	return def
}

func fvsMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

// fvsColor converts a color property to a CSS color string. The property is
// normally a plain "#RRGGBB" string, but gradient titles carry an object such
// as {"colors":[{"value":"#F5A623FF","position":"0"},...],"angle":0}; printing
// that directly would leak Go map syntax into the dashboard, so we fall back
// to the first gradient stop.
func fvsColor(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if stops, ok := t["colors"].([]any); ok {
			for _, sv := range stops {
				if s, ok := sv.(map[string]any); ok {
					if c := fvsStr(s, "value"); c != "" {
						return c
					}
				}
			}
		}
		if c := fvsStr(t, "value"); c != "" {
			return c
		}
	}
	return ""
}

func fvsTitleText(st *renderState, props map[string]any) (string, string, TextStyle) {
	// Titles keep their content under "value" (older builds used "values").
	// The value may be plain text or a formula such as =$area+"标题".
	text := fvsStr(props, "value")
	if text == "" {
		text = fvsStr(props, "values")
	}
	if strings.HasPrefix(text, "=") {
		text = stringOf(st.evalStandalone(text))
	} else {
		text = st.substitute(text)
	}
	var ts TextStyle
	ts.FontFamily = strings.Trim(fvsStr(props, "fontFamily"), `"'`)
	ts.Color = fvsColor(props["color"])
	ts.Align = fvsStr(props, "textAlign")
	ts.LetterSpacing = fvsNum(props["letterSpacing"], 0)
	if style := fvsMap(props, "style"); len(style) > 0 {
		ts.FontSize = fvsNum(style["fontSize"], 0)
		ts.FontWeight = fvsStr(style, "fontWeight")
		if ts.FontWeight == "" && fvsBool(style["bold"], false) {
			ts.FontWeight = "bold"
		}
		if v := fvsStr(style, "lineHeight"); v != "" {
			ts.LineHeight = v
		}
	}
	switch fvsStr(props, "textRank") {
	case "vertical-rl", "vertical-lr":
		ts.WritingMode = fvsStr(props, "textRank")
	}
	if ts.Align == "" {
		ts.Align = "left"
	}
	htmlText := strings.ReplaceAll(html.EscapeString(text), "\n", "<br/>")
	return text, htmlText, ts
}

func fvsRichText(st *renderState, props map[string]any) (string, string, TextStyle) {
	var ts TextStyle
	switch fvsStr(props, "verticalAlign") {
	case "start":
		ts.VAlign = "top"
	case "center", "middle":
		ts.VAlign = "middle"
	case "end":
		ts.VAlign = "bottom"
	}
	if v := fvsStr(props, "textAlign"); v != "" {
		ts.Align = v
	}
	// FineReport stores the rich-text runs either under "values" or, in
	// newer templates, under "value". Both hold the same run list.
	runs, _ := props["values"].([]any)
	if len(runs) == 0 {
		if list, ok := props["value"].([]any); ok {
			runs = list
		}
	}
	if len(runs) == 0 {
		plain := st.substitute(fvsStr(props, "value"))
		return plain, strings.ReplaceAll(html.EscapeString(plain), "\n", "<br/>"), ts
	}
	var plain strings.Builder
	var body strings.Builder
	first := true
	for _, rv := range runs {
		run, _ := rv.(map[string]any)
		if run == nil {
			continue
		}
		insert := fvsRunText(st, run)
		plain.WriteString(insert)
		attrs := fvsMap(run, "attributes")
		var css []string
		if first {
			ts.FontFamily = strings.Trim(fvsStr(attrs, "font"), `"'`)
			ts.FontSize = fvsNum(attrs["size"], 0)
			ts.Color = fvsStr(attrs, "color")
			ts.LineHeight = fvsStr(attrs, "lineHeight")
			first = false
		} else if fvsStr(attrs, "color") != ts.Color {
			// Runs disagree on color, so the widget-level color would paint
			// uncolored runs with the first run's color. Leave the widget
			// color unset and keep every run styled individually.
			ts.Color = ""
		}
		if f := strings.Trim(fvsStr(attrs, "font"), `"'`); f != "" {
			css = append(css, "font-family:"+safeCSS(f))
		}
		if size := fvsNum(attrs["size"], 0); size > 0 {
			css = append(css, fmt.Sprintf("font-size:%gpx", size))
		}
		if c := fvsStr(attrs, "color"); c != "" {
			css = append(css, "color:"+safeCSS(c))
		}
		if lh := fvsStr(attrs, "lineHeight"); lh != "" {
			css = append(css, "line-height:"+safeCSS(lh))
		}
		if fvsBool(attrs["bold"], false) {
			css = append(css, "font-weight:bold")
		}
		if fvsBool(attrs["italic"], false) {
			css = append(css, "font-style:italic")
		}
		if fvsBool(attrs["underline"], false) {
			css = append(css, "text-decoration:underline")
		}
		escaped := strings.ReplaceAll(html.EscapeString(insert), "\n", "<br/>")
		if len(css) > 0 {
			body.WriteString(`<span style="` + strings.Join(css, ";") + `">` + escaped + `</span>`)
		} else {
			body.WriteString(escaped)
		}
	}
	return plain.String(), body.String(), ts
}

// fvsRunText resolves the text of one rich-text run. The "insert" value is
// either a plain string or a formula object such as
// {"fr-formula": {"fx": "=sum(VALUE(...))"}}; printing the object directly
// would leak Go map syntax into the dashboard.
func fvsRunText(st *renderState, run map[string]any) string {
	switch ins := run["insert"].(type) {
	case string:
		return st.substitute(ins)
	case map[string]any:
		if f, ok := ins["fr-formula"].(map[string]any); ok {
			fx := strings.TrimSpace(fvsStr(f, "fx"))
			if fx == "" {
				return fvsStr(f, "text")
			}
			return stringOf(st.evalStandalone(fx))
		}
		if t, ok := ins["text"].(string); ok {
			return st.substitute(t)
		}
		return fvsStr(ins, "text")
	default:
		return st.substitute(fvsStr(run, "insert"))
	}
}

func fvsTimeText(props map[string]any) (string, TextStyle) {
	var ts TextStyle
	ts.FontFamily = strings.Trim(fvsStr(props, "fontFamily"), `"'`)
	ts.Color = fvsColor(props["color"])
	ts.Align = fvsStr(props, "textAlign")
	ts.LetterSpacing = fvsNum(props["letterSpacing"], 0)
	if style := fvsMap(props, "style"); len(style) > 0 {
		ts.FontSize = fvsNum(style["fontSize"], 0)
		ts.FontWeight = fvsStr(style, "fontWeight")
	}
	now := time.Now()
	var out string
	dt := fvsMap(props, "dateTime")
	if fvsBool(dt["enabled"], true) {
		layout := "yyyy-MM-dd"
		if fm := fvsMap(dt, "dateTimeFormat"); len(fm) > 0 {
			if v := fvsStr(fm, "value"); v != "" {
				layout = v
			}
		}
		out = now.Format(javaDateLayout(layout))
	}
	if wk := fvsMap(props, "week"); fvsBool(wk["enabled"], false) {
		names := []string{"日", "一", "二", "三", "四", "五", "六"}
		if out != "" {
			out += " "
		}
		out += "星期" + names[int(now.Weekday())]
	}
	return out, ts
}

func safeCSS(s string) string {
	s = strings.ReplaceAll(s, ";", "")
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "'", "")
	return s
}
