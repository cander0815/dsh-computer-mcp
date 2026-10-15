package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Agent, string) {
	t.Helper()
	root := t.TempDir()
	a, err := newAgent(Config{Name: "测试电脑", Token: strings.Repeat("a", 64), Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	return a, root
}
func invoke(a *Agent, name string, args any) (any, error) {
	b, _ := json.Marshal(args)
	return a.callTool(context.Background(), name, b)
}
func TestFileBoundaryAndConflict(t *testing.T) {
	a, root := fixture(t)
	_, err := invoke(a, "write_file", map[string]any{"root": rootID(root), "path": "a.txt", "text": "你好", "expected_sha256": "new"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := invoke(a, "read_file", map[string]any{"root": rootID(root), "path": "a.txt"})
	if err != nil || !strings.Contains(string(mustJSON(value)), "你好") {
		t.Fatalf("读写失败: %v", err)
	}
	_, err = invoke(a, "write_file", map[string]any{"root": rootID(root), "path": "a.txt", "text": "覆盖", "expected_sha256": "new"})
	if err == nil {
		t.Fatal("应拒绝覆盖冲突")
	}
	for _, path := range []string{"../secret.txt", filepath.Join(t.TempDir(), "outside.txt")} {
		if _, err = invoke(a, "read_file", map[string]any{"root": rootID(root), "path": path}); err == nil {
			t.Fatal("允许了越界路径")
		}
	}
	sum := sha256.Sum256([]byte("你好"))
	_, err = invoke(a, "write_file", map[string]any{"root": rootID(root), "path": "a.txt", "text": "更新", "expected_sha256": hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
}
func TestSymlinkEscape(t *testing.T) {
	a, root := fixture(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	for _, name := range []string{"read_file", "write_file"} {
		if _, err := invoke(a, name, map[string]any{"root": rootID(root), "path": "link/secret.txt", "text": "坏", "expected_sha256": "new"}); err == nil {
			t.Fatal("允许了符号链接越界")
		}
	}
}
func TestMCPAuthenticationAndOrigin(t *testing.T) {
	a, _ := fixture(t)
	cases := []struct {
		token, origin string
		want          int
	}{{"", "", 401}, {strings.Repeat("a", 64), "https://evil.invalid", 403}, {strings.Repeat("a", 64), "", 200}}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "http://127.0.0.1:18790/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`))
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Origin", c.origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.MCPHandler().ServeHTTP(w, req)
		if w.Code != c.want {
			t.Fatalf("响应 %d，应为 %d: %s", w.Code, c.want, w.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "http://evil.invalid/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	a.MCPHandler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatal("未拒绝恶意 Host")
	}
}
func TestExecutionDisabledByDefault(t *testing.T) {
	a, root := fixture(t)
	if _, err := invoke(a, "run_command", map[string]any{"root": rootID(root), "program": "echo", "args": []string{"不应执行"}}); err == nil {
		t.Fatal("默认允许了命令执行")
	}
}
func TestUnknownToolAndRoot(t *testing.T) {
	a, _ := fixture(t)
	if _, err := invoke(a, "unknown", map[string]any{}); err == nil {
		t.Fatal("未知工具应失败")
	}
	if _, err := invoke(a, "read_file", map[string]any{"root": "unknown", "path": "a"}); err == nil {
		t.Fatal("未知目录应失败")
	}
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestChangedRootSymlinkIsRejected(t *testing.T) {
	a, root := fixture(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("private"), 0600)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Skip(err)
	}
	if _, err := invoke(a, "read_file", map[string]any{"root": rootID(root), "path": "secret.txt"}); err == nil {
		t.Fatal("授权根目录被替换成链接后仍可读取")
	}
}

func TestCreateDirectoryInsideRoot(t *testing.T) {
	a, root := fixture(t)
	if _, err := invoke(a, "create_directory", map[string]any{"root": rootID(root), "path": "new-folder"}); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(a, "create_directory", map[string]any{"root": rootID(root), "path": "../outside"}); err == nil {
		t.Fatal("不应在外部创建文件夹")
	}
}
