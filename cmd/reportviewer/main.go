package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"qdake/reportviewer/internal/server"
)

func main() {
	defaultRoot := findWebroot()
	webroot := flag.String("webroot", defaultRoot, "FineReport webroot 目录（含 WEB-INF）")
	reportlets := flag.String("reportlets", "", "模板目录，默认 <webroot>/WEB-INF/reportlets")
	addr := flag.String("addr", ":8090", "HTTP 监听地址")
	allowSQL := flag.Bool("allow-formula-sql", false, "允许公式里的 SQL() 函数执行查询（默认关闭）")
	allowCrossReport := flag.Bool("allow-cross-report", false, "允许公式跨报表取值（默认关闭）")
	allowCrossSheet := flag.Bool("allow-cross-sheet", false, "允许公式跨工作表引用（默认关闭）")
	flag.Parse()

	srv, err := server.New(*webroot, *reportlets)
	if err != nil {
		log.Fatalf("启动失败: %v", err)
	}
	defer srv.Close()

	// The command line switches override the configuration file and the
	// environment, so an operator can tighten or loosen the policy without
	// editing files.
	p := srv.Policy()
	p.FormulaSQL = p.FormulaSQL || *allowSQL
	p.CrossReport = p.CrossReport || *allowCrossReport
	p.CrossSheet = p.CrossSheet || *allowCrossSheet
	srv.SetPolicy(p)
	log.Printf("安全防护: SQL公式=%v 跨报表=%v 跨表=%v",
		p.FormulaSQL, p.CrossReport, p.CrossSheet)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("报表查看器已启动: %s", browserURL(*addr))
	log.Printf("webroot:    %s", srv.Webroot)
	log.Printf("reportlets: %s", srv.Reportlets)

	// Graceful shutdown: on SIGINT/SIGTERM stop accepting new connections
	// and give the in-flight renders a moment to finish.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		log.Fatalf("服务异常退出: %v", err)
	case sig := <-stop:
		log.Printf("收到信号 %v，正在优雅停止…", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("停止超时，强制退出: %v", err)
			_ = httpSrv.Close()
		} else {
			log.Printf("已停止")
		}
	}
}

func browserURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1" + addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func findWebroot() string {
	if v := os.Getenv("FR_WEBROOT"); v != "" {
		return v
	}
	candidates := []string{
		`F:\tomcat-win64\webapps\webroot`,
		`C:\tomcat-win64\webapps\webroot`,
		`D:\tomcat-win64\webapps\webroot`,
		filepath.Join("webroot"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "WEB-INF")); err == nil && st.IsDir() {
			return c
		}
	}
	return candidates[0]
}
