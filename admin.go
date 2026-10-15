package main

import (
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed ui.html
var adminPage string

func (a *Agent) AdminHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(remote).IsLoopback() {
			http.Error(w, "管理页仅限本机", 403)
			return
		}
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "管理页地址无效", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "来源不允许", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		if r.URL.Path == "/" && r.Method == "GET" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(adminPage))
			return
		}
		if !tokenMatches(r.Header.Get("X-Admin-Token"), a.adminToken) {
			http.Error(w, "请使用程序显示的完整管理地址", 401)
			return
		}
		switch {
		case r.URL.Path == "/api/state" && r.Method == "GET":
			sendJSON(w, 200, map[string]any{"config": a.snapshot(), "addresses": localAddresses(), "port": a.port, "version": version, "platform": runtime.GOOS, "privilege": privilegeMode(), "connection": a.connectionState(), "log_directory": a.logDirectory(), "log_error": a.audit.errorText()})
		case r.URL.Path == "/api/connection" && r.Method == "GET":
			sendJSON(w, 200, map[string]any{"connection": a.connectionState(), "privilege": privilegeMode(), "log_error": a.audit.errorText()})
		case r.URL.Path == "/api/config" && r.Method == "POST":
			r.Body = http.MaxBytesReader(w, r.Body, 32768)
			var c Config
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				http.Error(w, "配置无效", 400)
				return
			}
			if err = a.update(c); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			sendJSON(w, 200, map[string]bool{"ok": true})
		case r.URL.Path == "/api/browse" && r.Method == "GET":
			path := r.URL.Query().Get("path")
			if path == "" {
				path, _ = os.UserHomeDir()
			}
			if !filepath.IsAbs(path) {
				http.Error(w, "请输入绝对目录", 400)
				return
			}
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			out := []any{}
			for _, e := range entries {
				if len(out) >= 1000 {
					break
				}
				if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
					out = append(out, map[string]string{"name": e.Name(), "path": filepath.Join(path, e.Name())})
				}
			}
			sendJSON(w, 200, map[string]any{"path": path, "parent": filepath.Dir(path), "directories": out})
		default:
			http.NotFound(w, r)
		}
	})
}
