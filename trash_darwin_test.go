package main

import (
	"context"
	"os"
	"path/filepath"

	"testing"
)

func TestNativeMacTrashAndRestore(t *testing.T) {
	if os.Getenv("DSH_TEST_NATIVE_TRASH") != "1" {
		t.Skip("仅在明确开启时使用真实废纸篓，并立即恢复测试文件")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh-回收测试-'-$()-"+randomToken()[:12]+".txt")
	if err := os.WriteFile(path, []byte("可恢复的临时测试文件"), 0600); err != nil {
		t.Fatal(err)
	}
	destination, err := systemTrash(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("原文件没有移走")
	}
	if destination != "macOS 废纸篓" {
		t.Fatal("不是废纸篓位置", destination)
	}
	home, _ := os.UserHomeDir()
	recovered := filepath.Join(home, ".Trash", filepath.Base(path))
	if err := os.Rename(recovered, path); err != nil {
		t.Fatal("恢复测试文件失败，请检查", destination, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "可恢复的临时测试文件" {
		t.Fatal("内容未保留")
	}
}
