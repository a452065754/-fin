package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCacheDisabled(t *testing.T) {
	c := NewPreviewCache(0, 10)
	if c.Enabled() {
		t.Error("ttl 为 0 时缓存应禁用")
	}
	c.Put("k", []byte("v"))
	if _, ok := c.Get("k"); ok {
		t.Error("禁用的缓存不应命中")
	}
}

func TestCacheHitMiss(t *testing.T) {
	c := NewPreviewCache(time.Minute, 10)
	if _, ok := c.Get("missing"); ok {
		t.Error("不存在的键不应命中")
	}
	c.Put("k", []byte("hello"))
	got, ok := c.Get("k")
	if !ok {
		t.Fatal("写入后应命中")
	}
	if string(got) != "hello" {
		t.Errorf("取回的内容错误: %q", got)
	}
	stats := c.Stats()
	if stats["hits"].(int64) != 1 || stats["misses"].(int64) != 1 {
		t.Errorf("命中/未命中统计错误: %v", stats)
	}
}

func TestCacheExpiry(t *testing.T) {
	c := NewPreviewCache(30*time.Millisecond, 10)
	c.Put("k", []byte("v"))
	if _, ok := c.Get("k"); !ok {
		t.Fatal("未过期时应命中")
	}
	time.Sleep(60 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Error("过期后不应命中")
	}
}

func TestCacheEviction(t *testing.T) {
	c := NewPreviewCache(time.Minute, 3)
	for i := 0; i < 5; i++ {
		c.Put(string(rune('a'+i)), []byte("v"))
	}
	stats := c.Stats()
	if stats["entries"].(int) > 3 {
		t.Errorf("条目数应不超过上限 3，实际 %v", stats["entries"])
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := NewPreviewCache(time.Minute, 10)
	c.Put("k", []byte("v"))
	c.Invalidate()
	if _, ok := c.Get("k"); ok {
		t.Error("清空后不应命中")
	}
}

// The cache key must depend on the parameters, otherwise two different
// parameter sets would share an entry.
func TestPreviewKeyDependsOnParams(t *testing.T) {
	a := previewKey("r.cpt", map[string]string{"日期": "2026-01-01"})
	b := previewKey("r.cpt", map[string]string{"日期": "2026-01-02"})
	if a == b {
		t.Error("不同参数应产生不同的缓存键")
	}
	// The same parameters in a different order must give the same key.
	c := previewKey("r.cpt", map[string]string{"a": "1", "b": "2"})
	d := previewKey("r.cpt", map[string]string{"b": "2", "a": "1"})
	if c != d {
		t.Error("参数顺序不应影响缓存键")
	}
	// A different path must give a different key.
	e := previewKey("other.cpt", map[string]string{"a": "1", "b": "2"})
	if c == e {
		t.Error("不同模板应产生不同的缓存键")
	}
}

// The preview endpoint must serve the second identical request from the cache.
func TestPreviewUsesCache(t *testing.T) {
	os.Unsetenv("RV_CACHE_TTL")
	s, err := New(testWebroot, "")
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	defer s.Close()
	// A fresh cache with a long lifetime.
	s.cache = NewPreviewCache(time.Minute, 8)

	req := httptest.NewRequest("GET", "/api/preview?path=demo/basic/%E5%88%86%E7%BB%84%E7%BB%9F%E8%AE%A1.cpt", nil)
	rec1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("首次请求应返回 200，实际 %d", rec1.Code)
	}
	if rec1.Header().Get("X-Cache") != "MISS" {
		t.Errorf("首次请求应为 MISS，实际 %q", rec1.Header().Get("X-Cache"))
	}
	first := rec1.Body.String()

	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("第二次请求应返回 200，实际 %d", rec2.Code)
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("第二次请求应为 HIT，实际 %q", rec2.Header().Get("X-Cache"))
	}
	if rec2.Body.String() != first {
		t.Error("缓存返回的内容与首次渲染不一致")
	}
	if rec2.Header().Get("X-Render-Ms") != "0" {
		t.Errorf("缓存命中的渲染耗时应为 0，实际 %q", rec2.Header().Get("X-Render-Ms"))
	}
}

// Different parameters must not share a cached entry.
func TestPreviewCacheSeparatesParams(t *testing.T) {
	s, err := New(testWebroot, "")
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	defer s.Close()
	s.cache = NewPreviewCache(time.Minute, 8)

	req1 := httptest.NewRequest("GET", "/api/preview?path=demo/basic/%E5%88%86%E7%BB%84%E7%BB%9F%E8%AE%A1.cpt&x=1", nil)
	rec1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "/api/preview?path=demo/basic/%E5%88%86%E7%BB%84%E7%BB%9F%E8%AE%A1.cpt&x=2", nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Header().Get("X-Cache") != "MISS" {
		t.Errorf("不同参数应重新渲染，实际 %q", rec2.Header().Get("X-Cache"))
	}
}
