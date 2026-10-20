package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditLogMemoryOnly(t *testing.T) {
	a, err := NewAuditLog("")
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	if a.Enabled() {
		t.Error("没有配置路径时不应启用文件写入")
	}
	a.Write(AuditEntry{Action: "preview", Path: "a.cpt", Status: 200})
	entries := a.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("应有 1 条记录，实际 %d", len(entries))
	}
	if entries[0].Path != "a.cpt" {
		t.Errorf("记录内容错误: %+v", entries[0])
	}
	if entries[0].Time == "" {
		t.Error("记录应自动填入时间")
	}
}

func TestAuditLogWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "audit.log")
	a, err := NewAuditLog(path)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer a.Close()
	if !a.Enabled() {
		t.Error("配置了路径后应启用文件写入")
	}
	a.Write(AuditEntry{Action: "preview", Path: "x.cpt", User: "bob", Status: 200, CostMs: 12})
	a.Write(AuditEntry{Action: "preview", Path: "y.cpt", User: "amy", Status: 400, CostMs: 3})
	a.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("应有 2 行日志，实际 %d: %q", len(lines), string(data))
	}
	// Each line must be a complete JSON object.
	for i, line := range lines {
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("第 %d 行不是合法 JSON: %v (%q)", i+1, err, line)
		}
	}
	if !strings.Contains(string(data), "bob") || !strings.Contains(string(data), "amy") {
		t.Errorf("日志缺少用户名: %q", string(data))
	}
}

func TestAuditLogRecentLimit(t *testing.T) {
	a, _ := NewAuditLog("")
	for i := 0; i < 10; i++ {
		a.Write(AuditEntry{Path: string(rune('a' + i))})
	}
	entries := a.Recent(3)
	if len(entries) != 3 {
		t.Fatalf("应返回 3 条，实际 %d", len(entries))
	}
	// The newest entries come last.
	if entries[2].Path != "j" {
		t.Errorf("最后一条应是最新记录 j，实际 %q", entries[2].Path)
	}
}

func TestFormatParams(t *testing.T) {
	if got := formatParams(nil); got != "" {
		t.Errorf("空参数应返回空串，实际 %q", got)
	}
	got := formatParams(map[string]string{"b": "2", "a": "1"})
	if got != "a=1&b=2" {
		t.Errorf("参数应按名字排序，实际 %q", got)
	}
}

// The preview endpoint must produce an audit entry.
func TestPreviewIsAudited(t *testing.T) {
	s, err := New(testWebroot, "")
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	defer s.Close()

	req := httptest.NewRequest("GET", "/api/preview?path=demo/basic/%E5%88%86%E7%BB%84%E7%BB%9F%E8%AE%A1.cpt&%E5%8C%BA%E5%9F%9F=x", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries := s.audit.Recent(10)
	if len(entries) == 0 {
		t.Fatal("预览请求应产生审计记录")
	}
	last := entries[len(entries)-1]
	if last.Action != "preview" {
		t.Errorf("动作应为 preview，实际 %q", last.Action)
	}
	if !strings.Contains(last.Path, "分组统计") {
		t.Errorf("记录里应包含模板路径，实际 %q", last.Path)
	}
	if !strings.Contains(last.Params, "区域=x") {
		t.Errorf("记录里应包含参数，实际 %q", last.Params)
	}
	if last.Status != http.StatusOK {
		t.Errorf("状态码应为 200，实际 %d", last.Status)
	}
}

// The audit API must return the recorded entries.
func TestAuditEndpoint(t *testing.T) {
	s, err := New(testWebroot, "")
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	defer s.Close()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/audit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("审计接口应返回 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "entries") {
		t.Errorf("审计接口响应缺少 entries: %s", body)
	}
}
