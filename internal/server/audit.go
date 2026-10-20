package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// AuditEntry is one recorded access.
type AuditEntry struct {
	Time    string `json:"time"`
	User    string `json:"user,omitempty"`
	Address string `json:"address,omitempty"`
	Action  string `json:"action"`
	Path    string `json:"path,omitempty"`
	Params  string `json:"params,omitempty"`
	Status  int    `json:"status"`
	CostMs  int64  `json:"costMs"`
}

// AuditLog records who looked at which report. It writes one JSON object per
// line so the file can be tailed and grepped without extra tooling.
type AuditLog struct {
	mu      sync.Mutex
	file    *os.File
	enabled bool
	// fallback is used when no log file is configured: the entries are kept
	// in memory and exposed through the API instead.
	memory []AuditEntry
	maxMem int
}

// NewAuditLog opens the audit log. The path may be empty, in which case the
// entries are only kept in memory and served by the API.
func NewAuditLog(path string) (*AuditLog, error) {
	a := &AuditLog{maxMem: 500}
	if path == "" {
		return a, nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建审计日志目录失败: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开审计日志失败: %w", err)
	}
	a.file = f
	a.enabled = true
	return a, nil
}

// Enabled reports whether entries are written to a file.
func (a *AuditLog) Enabled() bool { return a != nil && a.enabled }

// Close releases the log file.
func (a *AuditLog) Close() {
	if a == nil || a.file == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.file.Close()
	a.file = nil
	a.enabled = false
}

// Write records one entry.
func (a *AuditLog) Write(e AuditEntry) {
	if a == nil {
		return
	}
	if e.Time == "" {
		e.Time = time.Now().Format(time.RFC3339)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Keep the recent entries in memory so the API can show them.
	a.memory = append(a.memory, e)
	if len(a.memory) > a.maxMem {
		a.memory = a.memory[len(a.memory)-a.maxMem:]
	}
	if a.file == nil {
		return
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = a.file.Write(append(line, '\n'))
}

// Recent returns the most recent entries, newest last.
func (a *AuditLog) Recent(limit int) []AuditEntry {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if limit <= 0 || limit > len(a.memory) {
		limit = len(a.memory)
	}
	out := make([]AuditEntry, limit)
	copy(out, a.memory[len(a.memory)-limit:])
	return out
}

// AuditUser returns the authenticated user name, or an empty string when
// authentication is off.
func (s *Server) AuditUser(r *http.Request) string {
	if !s.auth.Enabled() {
		return ""
	}
	user, _, ok := r.BasicAuth()
	if !ok {
		return ""
	}
	return user
}

// auditRecord writes an audit entry for a request.
func (s *Server) auditRecord(r *http.Request, action, path, params string, status int, cost time.Duration) {
	if s.audit == nil {
		return
	}
	s.audit.Write(AuditEntry{
		User:    s.AuditUser(r),
		Address: clientIP(r),
		Action:  action,
		Path:    path,
		Params:  params,
		Status:  status,
		CostMs:  cost.Milliseconds(),
	})
}

// clientIP returns the remote address without the port.
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := r.RemoteAddr
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

// formatParams renders the parameters for the audit log in a compact form.
func formatParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += "&"
		}
		out += k + "=" + params[k]
	}
	return out
}
