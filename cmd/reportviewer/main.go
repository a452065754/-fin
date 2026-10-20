package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"qdake/reportviewer/internal/server"
)

func main() {
	defaultRoot := findWebroot()
	webroot := flag.String("webroot", defaultRoot, "FineReport webroot 目录（含 WEB-INF）")
	reportlets := flag.String("reportlets", "", "模板目录，默认 <webroot>/WEB-INF/reportlets")
	addr := flag.String("addr", ":8090", "HTTP 监听地址")
	flag.Parse()

	srv, err := server.New(*webroot, *reportlets)
	if err != nil {
		log.Fatalf("启动失败: %v", err)
	}
	defer srv.Close()

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("报表查看器已启动: %s", browserURL(*addr))
	log.Printf("webroot:    %s", srv.Webroot)
	log.Printf("reportlets: %s", srv.Reportlets)
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatal(err)
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
