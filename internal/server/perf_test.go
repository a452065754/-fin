package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPerfStatsObserve(t *testing.T) {
	p := NewPerfStats(10)
	p.Observe("a.cpt", 100*time.Millisecond, false, false)
	p.Observe("a.cpt", 300*time.Millisecond, false, false)
	p.Observe("a.cpt", 0, true, false)

	snap := p.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("应有 1 个模板记录，实际 %d", len(snap))
	}
	s := snap[0]
	if s.Path != "a.cpt" {
		t.Errorf("路径错误: %q", s.Path)
	}
	if s.Count != 3 {
		t.Errorf("次数应为 3，实际 %d", s.Count)
	}
	if s.TotalMs != 400 {
		t.Errorf("累计耗时应为 400ms，实际 %d", s.TotalMs)
	}
	if s.MaxMs != 300 {
		t.Errorf("最慢应为 300ms，实际 %d", s.MaxMs)
	}
	if s.Hits != 1 || s.Misses != 2 {
		t.Errorf("命中/未命中统计错误: hits=%d misses=%d", s.Hits, s.Misses)
	}
	if s.HitRate < 0.3 || s.HitRate > 0.4 {
		t.Errorf("命中率应在 0.33 附近，实际 %v", s.HitRate)
	}
	if s.AvgMs < 130 || s.AvgMs > 140 {
		t.Errorf("平均耗时应约 133ms，实际 %v", s.AvgMs)
	}
}

func TestPerfStatsSortedByTime(t *testing.T) {
	p := NewPerfStats(10)
	p.Observe("fast.cpt", 10*time.Millisecond, false, false)
	p.Observe("slow.cpt", 900*time.Millisecond, false, false)
	p.Observe("mid.cpt", 100*time.Millisecond, false, false)

	snap := p.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("应有 3 条记录，实际 %d", len(snap))
	}
	if snap[0].Path != "slow.cpt" {
		t.Errorf("最慢的应排第一，实际 %q", snap[0].Path)
	}
	if snap[2].Path != "fast.cpt" {
		t.Errorf("最快的应排最后，实际 %q", snap[2].Path)
	}
}

func TestPerfStatsErrors(t *testing.T) {
	p := NewPerfStats(10)
	p.Observe("bad.cpt", 5*time.Millisecond, false, true)
	snap := p.Snapshot()
	if len(snap) != 1 || snap[0].Errors != 1 {
		t.Errorf("错误数应记为 1，实际 %+v", snap)
	}
}

// The collector must not grow without bound.
func TestPerfStatsLimit(t *testing.T) {
	p := NewPerfStats(5)
	for i := 0; i < 20; i++ {
		p.Observe(string(rune('a'+i))+".cpt", time.Millisecond, false, false)
	}
	if p.Total() > 5 {
		t.Errorf("跟踪的模板数不应超过上限 5，实际 %d", p.Total())
	}
}

// The perf endpoint must report the collected statistics.
func TestPerfEndpoint(t *testing.T) {
	s, err := New(testWebroot, "")
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	defer s.Close()

	// Render one template so there is something to report.
	req := httptest.NewRequest("GET", "/api/preview?path=demo/basic/%E5%88%86%E7%BB%84%E7%BB%9F%E8%AE%A1.cpt", nil)
	s.Handler().ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/perf", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("性能接口应返回 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !contains(body, "templates") {
		t.Errorf("响应缺少 templates 字段: %s", body)
	}
	if !contains(body, "分组统计") {
		t.Errorf("响应里应包含刚渲染的模板: %s", body)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
