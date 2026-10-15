package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAdminIsLocalAndAuthenticated(t *testing.T) {
	a, _ := fixture(t)
	for _, c := range []struct {
		token, origin, remote string
		want                  int
	}{{"", "", "127.0.0.1:1000", 401}, {a.adminToken, "http://evil.invalid", "127.0.0.1:1000", 403}, {a.adminToken, "", "192.168.1.2:1000", 403}, {a.adminToken, "", "127.0.0.1:1000", 200}} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:18791/api/state", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Admin-Token", c.token)
		r.Header.Set("Origin", c.origin)
		w := httptest.NewRecorder()
		a.AdminHandler().ServeHTTP(w, r)
		if w.Code != c.want {
			t.Fatalf("管理接口状态 %d，应为 %d", w.Code, c.want)
		}
	}
}
func TestCommandTimeoutAndOutput(t *testing.T) {
	a, root := fixture(t)
	cfg := a.snapshot()
	cfg.AllowExec = true
	a.update(cfg)
	program := "sh"
	args := []string{"-c", "printf hello; sleep 3"}
	if runtime.GOOS == "windows" {
		program = "powershell.exe"
		args = []string{"-NoProfile", "-Command", "Write-Output hello; Start-Sleep -Seconds 3"}
	}
	b, _ := json.Marshal(map[string]any{"root": rootID(root), "program": program, "args": args, "timeout_seconds": 1})
	start := time.Now()
	v, err := a.callTool(context.Background(), "run_command", b)
	if err != nil {
		t.Fatal(err)
	}
	out := string(mustJSON(v))
	if !strings.Contains(out, "hello") || !strings.Contains(out, `"timed_out":true`) {
		t.Fatal(out)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("超时未终止进程")
	}
}

func TestShutdownHandlesTermination(t *testing.T) {
	found := false
	for _, signal := range shutdownSignals() {
		if signal.String() == "terminated" {
			found = true
		}
	}
	if !found {
		t.Fatal("程序关闭应处理终止信号，取消正在执行的任务")
	}
}
