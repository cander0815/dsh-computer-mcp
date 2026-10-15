package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDailyLogRotation(t *testing.T) {
	dir := t.TempDir()
	logger := newDailyLogger(dir, "")
	now := time.Date(2026, 10, 14, 23, 59, 59, 0, time.Local)
	logger.now = func() time.Time { return now }
	logger.write(map[string]any{"event": "连接成功"})
	now = now.Add(time.Second)
	logger.write(map[string]any{"event": "操作完成"})
	for _, day := range []string{"2026-10-14", "2026-10-15"} {
		b, err := os.ReadFile(filepath.Join(dir, day+".log"))
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		if json.Unmarshal(b, &row) != nil || !strings.HasPrefix(row["time"].(string), day) {
			t.Fatal(string(b))
		}
	}
}

func TestMCPActivityAndAudit(t *testing.T) {
	a, root := fixture(t)
	dir := t.TempDir()
	a.audit = newDailyLogger(dir, "")
	request := func(method, params, token string) {
		r := httptest.NewRequest("POST", "http://127.0.0.1:18790/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		a.MCPHandler().ServeHTTP(httptest.NewRecorder(), r)
	}
	request("initialize", `{}`, "invalid")
	if a.connectionState().LastSeen != "" {
		t.Fatal("未认证请求不应算连接")
	}
	request("initialize", `{"clientInfo":{"name":"测试客户端"}}`, a.snapshot().Token)
	if a.connectionState().Status != "recent" {
		t.Fatal("连接状态未更新")
	}
	params := string(mustJSON(map[string]any{"name": "write_file", "arguments": map[string]any{"root": rootID(root), "path": "proof.txt", "text": "不能写入日志的正文", "expected_sha256": "new"}}))
	request("tools/call", params, a.snapshot().Token)
	request("tools/call", params, a.snapshot().Token)
	b, err := os.ReadFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"连接成功", "write_file", "proof.txt", "失败"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("日志缺少 " + want)
		}
	}
	for _, secret := range []string{a.snapshot().Token, "不能写入日志的正文"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("日志泄漏正文或令牌")
		}
	}
	if a.connectionState().ActiveRequests != 0 {
		t.Fatal("活跃请求未归零")
	}
}

func TestCommandAuditContainsResultAndRedactsNamedSecrets(t *testing.T) {
	a, _ := fixture(t)
	dir := t.TempDir()
	a.audit = newDailyLogger(dir, "")
	raw := mustJSON(map[string]any{"program": "test", "args": []string{"--token", "secret-token", "--password=secret-password"}})
	a.auditTool("run_command", raw, map[string]any{"output": "命令执行结果", "exit_code": 1, "timed_out": false}, nil)
	b, err := os.ReadFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"命令执行结果", "exit_code", "失败"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("缺少命令结果")
		}
	}
	if strings.Contains(string(b), "secret-token") || strings.Contains(string(b), "secret-password") {
		t.Fatal("未隐藏敏感参数")
	}
}
func TestActiveAndIdleConnectionState(t *testing.T) {
	a, _ := fixture(t)
	finish := a.beginRequest("127.0.0.1:1")
	if a.connectionState().Status != "active" {
		t.Fatal("没有显示活跃请求")
	}
	finish()
	a.activity.mu.Lock()
	a.activity.lastSeen = time.Now().Add(-2 * time.Minute)
	a.activity.mu.Unlock()
	if a.connectionState().Status != "idle" {
		t.Fatal("空闲状态错误")
	}
}
func TestConfigReplacementPreservesOwner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Config{Name: "所有权测试", Token: strings.Repeat("a", 64)}
	if err := saveConfig(p, c); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(p, c); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(p); err != nil {
		t.Fatal(err)
	}
}
