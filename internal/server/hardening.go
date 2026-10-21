package server

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// contextWithTimeout is a small helper so the timeout wiring stays readable.
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// Auth guards the HTTP endpoints. It is off by default so a single-user
// desktop run needs no setup, and it can be turned on with a password
// environment variable.
type Auth struct {
	// user and password are empty when authentication is disabled.
	user     string
	password string
	enabled  bool
}

// NewAuth reads the credentials from the environment. Both variables must be
// set for authentication to be enabled.
//
//	RV_USER   the user name
//	RV_PASSWORD  the password (compared in constant time)
func NewAuth() *Auth {
	user := os.Getenv("RV_USER")
	pass := os.Getenv("RV_PASSWORD")
	a := &Auth{user: user, password: pass}
	a.enabled = user != "" && pass != ""
	return a
}

// Enabled reports whether authentication is required.
func (a *Auth) Enabled() bool { return a != nil && a.enabled }

// Check validates the credentials from an HTTP request.
func (a *Auth) Check(r *http.Request) bool {
	if !a.Enabled() {
		return true
	}
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	// Constant time comparison so the password cannot be guessed by timing.
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(a.user)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(a.password)) == 1
	return userOK && passOK
}

// Middleware wraps a handler with basic authentication.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	if !a.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Check(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="reportviewer"`)
			http.Error(w, "需要登录", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Metrics keeps a few counters that the health endpoint reports.
type Metrics struct {
	mu       sync.Mutex
	requests int64
	errors   int64
	slow     int64
	started  time.Time
}

// NewMetrics creates a counter set.
func NewMetrics() *Metrics {
	return &Metrics{started: time.Now()}
}

// Observe records one request.
func (m *Metrics) Observe(err error, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests++
	if err != nil {
		m.errors++
	}
	// A request slower than the slow threshold is worth surfacing: it usually
	// means a slow database or a heavy template.
	if elapsed > 3*time.Second {
		m.slow++
	}
}

// Snapshot returns the current counters.
func (m *Metrics) Snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"requests":      m.requests,
		"errors":        m.errors,
		"slow":          m.slow,
		"uptimeSeconds": int64(time.Since(m.started).Seconds()),
	}
}

// statusRecorder captures the response status so errors can be counted.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Middleware counts requests and applies a per-request timeout.
func (m *Metrics) Middleware(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		if timeout > 0 {
			// The context deadline makes long running handlers give up; the
			// response is written as a gateway timeout so the browser shows
			// a clear error instead of hanging.
			ctx, cancel := contextWithTimeout(r, timeout)
			defer cancel()
			next.ServeHTTP(rec, r.WithContext(ctx))
		} else {
			next.ServeHTTP(rec, r)
		}
		err := error(nil)
		if rec.status >= 500 {
			err = errServer
		}
		m.Observe(err, time.Since(start))
	})
}

var errServer = &serverError{}

type serverError struct{}

func (e *serverError) Error() string { return "server error" }

// writeJSONError writes a JSON error body.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// healthHandler reports the state of the service and its data sources.
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	conns := s.dr.Connections().List()
	items := make([]map[string]any, 0, len(conns))
	ready := 0
	for _, c := range conns {
		ok := !c.NeedsPassword || c.Resolved
		if ok {
			ready++
		}
		items = append(items, map[string]any{
			"name":     c.Name,
			"kind":     c.Kind,
			"ready":    ok,
			"resolved": c.Resolved,
		})
	}
	body := map[string]any{
		"status":      "ok",
		"webroot":     s.Webroot,
		"reportlets":  s.Reportlets,
		"connections": items,
		"ready":       ready,
		"total":       len(conns),
		"metrics":     s.metrics.Snapshot(),
		"cache":       s.cache.Stats(),
		"policy":      s.Policy().Summary(),
	}
	writeJSON(w, http.StatusOK, body)
}

// parseDurationEnv reads a duration from an environment variable.
func parseDurationEnv(name string, def time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	// Also accept a plain number of seconds.
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return def
}

// parseIntEnv reads an integer from an environment variable.
func parseIntEnv(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return def
}

// parseBoolEnv reads a boolean from an environment variable.
func parseBoolEnv(name string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
