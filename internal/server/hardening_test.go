package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// testWebroot points at the FineReport installation used by the tests.
const testWebroot = `F:/tomcat-win64/webapps/webroot`

func TestAuthDisabledByDefault(t *testing.T) {
	os.Unsetenv("RV_USER")
	os.Unsetenv("RV_PASSWORD")
	a := NewAuth()
	if a.Enabled() {
		t.Error("未配置凭据时不应启用认证")
	}
	// Without authentication every request passes.
	req := httptest.NewRequest("GET", "/api/health", nil)
	if !a.Check(req) {
		t.Error("未启用认证时应放行")
	}
}

func TestAuthEnabled(t *testing.T) {
	os.Setenv("RV_USER", "viewer")
	os.Setenv("RV_PASSWORD", "s3cret")
	defer func() {
		os.Unsetenv("RV_USER")
		os.Unsetenv("RV_PASSWORD")
	}()
	a := NewAuth()
	if !a.Enabled() {
		t.Fatal("配置了凭据后应启用认证")
	}
	// Right credentials pass.
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.SetBasicAuth("viewer", "s3cret")
	if !a.Check(req) {
		t.Error("正确凭据应放行")
	}
	// Wrong password fails.
	req2 := httptest.NewRequest("GET", "/api/health", nil)
	req2.SetBasicAuth("viewer", "wrong")
	if a.Check(req2) {
		t.Error("错误口令应拒绝")
	}
	// Wrong user fails.
	req3 := httptest.NewRequest("GET", "/api/health", nil)
	req3.SetBasicAuth("nobody", "s3cret")
	if a.Check(req3) {
		t.Error("错误用户名应拒绝")
	}
	// No credentials at all fails.
	req4 := httptest.NewRequest("GET", "/api/health", nil)
	if a.Check(req4) {
		t.Error("缺少凭据应拒绝")
	}
}

// Only one of the two variables set must not enable authentication, otherwise
// a half-configured service would lock everybody out.
func TestAuthNeedsBothVariables(t *testing.T) {
	os.Setenv("RV_USER", "viewer")
	os.Unsetenv("RV_PASSWORD")
	defer os.Unsetenv("RV_USER")
	if NewAuth().Enabled() {
		t.Error("只设了用户名时不应启用认证")
	}
}

func TestAuthMiddleware(t *testing.T) {
	os.Setenv("RV_USER", "viewer")
	os.Setenv("RV_PASSWORD", "s3cret")
	defer func() {
		os.Unsetenv("RV_USER")
		os.Unsetenv("RV_PASSWORD")
	}()
	called := false
	h := NewAuth().Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	// Without credentials the middleware answers 401 and the handler is not
	// reached.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("缺少凭据应返回 401，实际 %d", rec.Code)
	}
	if called {
		t.Error("未通过认证时不应调用处理函数")
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("401 响应缺少 WWW-Authenticate 头")
	}
	// With credentials the handler runs.
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.SetBasicAuth("viewer", "s3cret")
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Errorf("正确凭据应返回 200，实际 %d", rec2.Code)
	}
	if !called {
		t.Error("通过认证后应调用处理函数")
	}
}

func TestMetricsCounting(t *testing.T) {
	m := NewMetrics()
	m.Observe(nil, 10*time.Millisecond)
	m.Observe(errServer, 10*time.Millisecond)
	m.Observe(nil, 5*time.Second)
	snap := m.Snapshot()
	if snap["requests"].(int64) != 3 {
		t.Errorf("请求数应为 3，实际 %v", snap["requests"])
	}
	if snap["errors"].(int64) != 1 {
		t.Errorf("错误数应为 1，实际 %v", snap["errors"])
	}
	if snap["slow"].(int64) != 1 {
		t.Errorf("慢请求数应为 1，实际 %v", snap["slow"])
	}
}

func TestParseDurationEnv(t *testing.T) {
	os.Unsetenv("RV_TEST_DUR")
	if got := parseDurationEnv("RV_TEST_DUR", 30*time.Second); got != 30*time.Second {
		t.Errorf("未设置时应返回默认值，实际 %v", got)
	}
	os.Setenv("RV_TEST_DUR", "90s")
	defer os.Unsetenv("RV_TEST_DUR")
	if got := parseDurationEnv("RV_TEST_DUR", 30*time.Second); got != 90*time.Second {
		t.Errorf("应解析 90s，实际 %v", got)
	}
	// A plain number is read as seconds.
	os.Setenv("RV_TEST_DUR", "45")
	if got := parseDurationEnv("RV_TEST_DUR", 30*time.Second); got != 45*time.Second {
		t.Errorf("纯数字应按秒解析，实际 %v", got)
	}
	// Garbage falls back to the default.
	os.Setenv("RV_TEST_DUR", "abc")
	if got := parseDurationEnv("RV_TEST_DUR", 30*time.Second); got != 30*time.Second {
		t.Errorf("非法值应回退默认值，实际 %v", got)
	}
}

func TestParseBoolEnv(t *testing.T) {
	os.Unsetenv("RV_TEST_BOOL")
	if !parseBoolEnv("RV_TEST_BOOL", true) {
		t.Error("未设置时应返回默认值 true")
	}
	for _, v := range []string{"1", "true", "TRUE", "yes", "on"} {
		os.Setenv("RV_TEST_BOOL", v)
		if !parseBoolEnv("RV_TEST_BOOL", false) {
			t.Errorf("%q 应解析为 true", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off"} {
		os.Setenv("RV_TEST_BOOL", v)
		if parseBoolEnv("RV_TEST_BOOL", true) {
			t.Errorf("%q 应解析为 false", v)
		}
	}
	defer os.Unsetenv("RV_TEST_BOOL")
}

// The health endpoint must report the data sources and the counters.
func TestHealthEndpoint(t *testing.T) {
	s, err := New(testWebroot, "")
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	defer s.Close()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("健康检查应返回 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"status", "connections", "metrics", "webroot"} {
		if !strings.Contains(body, want) {
			t.Errorf("健康检查响应缺少字段 %q: %s", want, body)
		}
	}
}

// A graceful shutdown must let an in-flight request finish instead of cutting
// it off.
func TestGracefulShutdown(t *testing.T) {
	s, err := New(testWebroot, "")
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	defer s.Close()
	httpSrv := &http.Server{Handler: s.Handler()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	// A request that is in flight while the shutdown starts.
	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
		done <- rec.Code
	}()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		t.Fatalf("优雅停止失败: %v", err)
	}
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("在途请求应正常完成，实际 %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Error("在途请求没有完成")
	}
	// The listener must be closed after the shutdown.
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("停止后服务应返回 ErrServerClosed，实际 %v", err)
	}
}
