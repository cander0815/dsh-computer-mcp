package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteFileUsesTrashAndRemindsUser(t *testing.T) {
	a, root := fixture(t)
	p := filepath.Join(root, "delete.txt")
	os.WriteFile(p, []byte("测试"), 0600)
	called := false
	a.trash = func(ctx context.Context, path string) (string, error) {
		called = true
		expected, _ := filepath.EvalSymlinks(p)
		if path != expected {
			t.Fatal(path)
		}
		return "系统废纸篓", nil
	}
	value, err := invoke(a, "delete_file", map[string]any{"root": rootID(root), "path": "delete.txt"})
	if err != nil || !called {
		t.Fatal(err)
	}
	text := string(mustJSON(value))
	if !strings.Contains(text, "清空") || !strings.Contains(text, "提醒用户") {
		t.Fatal(text)
	}
	b, _ := json.Marshal(toolDefinitions(false))
	if !strings.Contains(string(b), "delete_file") {
		t.Fatal("文件删除不应依赖命令开关")
	}
}
func TestDeleteFileRejectsUnsafeTargetsAndNeverFallsBack(t *testing.T) {
	a, root := fixture(t)
	p := filepath.Join(root, "keep.txt")
	os.WriteFile(p, []byte("保留"), 0600)
	called := 0
	a.trash = func(context.Context, string) (string, error) { called++; return "", errors.New("回收站不可用") }
	for _, path := range []string{".", "../outside", filepath.Join(root, "keep.txt")} {
		if _, err := invoke(a, "delete_file", map[string]any{"root": rootID(root), "path": path}); err == nil {
			t.Fatal("接受危险目标", path)
		}
	}
	if called != 0 {
		t.Fatal("危险目标调用了系统回收站")
	}
	if _, err := invoke(a, "delete_file", map[string]any{"root": rootID(root), "path": "keep.txt"}); err == nil {
		t.Fatal("系统失败未传递")
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "保留" {
		t.Fatal("删除失败后文件丢失")
	}
	if os.Symlink(p, filepath.Join(root, "link")) == nil {
		if _, err := invoke(a, "delete_file", map[string]any{"root": rootID(root), "path": "link"}); err == nil {
			t.Fatal("接受符号链接")
		}
	}
	if called != 1 {
		t.Fatal("系统调用次数错误")
	}
}
