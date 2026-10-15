//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBackgroundChildIsCleanedUp(t *testing.T) {
	a, root := fixture(t)
	c := a.snapshot()
	c.AllowExec = true
	a.update(c)
	invoke(a, "run_command", map[string]any{"root": rootID(root), "program": "sh", "args": []string{"-c", "sleep 60 & echo $! > child.pid"}, "timeout_seconds": 10})
	data, err := os.ReadFile(filepath.Join(root, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	for i := 0; i < 20; i++ {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("命令返回后仍遗留后台子进程")
}

func TestFIFOIsRejectedWithoutBlocking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(p, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := readText(p); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("特殊文件未被拒绝")
		}
	case <-time.After(300 * time.Millisecond):
		// 打开写端释放被阻塞的读取，避免遗留测试协程。
		f, _ := os.OpenFile(p, os.O_WRONLY, 0600)
		if f != nil {
			f.Close()
		}
		t.Fatal("读取 FIFO 被阻塞")
	}
}

func TestRootEditingPreservesUserOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("此回归测试须以 root 执行")
	}
	a, root := fixture(t)
	if err := os.Chown(root, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "private.txt")
	if err := os.WriteFile(p, []byte("原内容"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(p, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(a, "write_file", map[string]any{"root": rootID(root), "path": "private.txt", "text": "新内容", "expected_sha256": digest([]byte("原内容"))}); err != nil {
		t.Fatal(err)
	}
	check := func(path string) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Sys().(*syscall.Stat_t).Uid != 65534 {
			t.Fatal("管理员操作改变了用户文件的所有者：", path)
		}
	}
	check(p)
	if _, err := invoke(a, "write_file", map[string]any{"root": rootID(root), "path": "new.txt", "text": "新建", "expected_sha256": "new"}); err != nil {
		t.Fatal(err)
	}
	check(filepath.Join(root, "new.txt"))
	if _, err := invoke(a, "create_directory", map[string]any{"root": rootID(root), "path": "newdir"}); err != nil {
		t.Fatal(err)
	}
	check(filepath.Join(root, "newdir"))
	// 自动补建的每一级目录、edit_file 和 move_file 的结果，都要保持用户所有。
	if _, err := invoke(a, "write_file", map[string]any{"root": rootID(root), "path": "a/b/deep.txt", "text": "深层", "expected_sha256": "new"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a", "a/b", "a/b/deep.txt"} {
		check(filepath.Join(root, path))
	}
	if _, err := invoke(a, "edit_file", map[string]any{"root": rootID(root), "path": "a/b/deep.txt", "old_string": "深层", "new_string": "已编辑"}); err != nil {
		t.Fatal(err)
	}
	check(filepath.Join(root, "a/b/deep.txt"))
	if _, err := invoke(a, "move_file", map[string]any{"root": rootID(root), "path": "a/b/deep.txt", "destination": "m/n/moved.txt"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"m", "m/n", "m/n/moved.txt"} {
		check(filepath.Join(root, path))
	}
	if _, err := invoke(a, "create_directory", map[string]any{"root": rootID(root), "path": "x/y/z"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"x", "x/y", "x/y/z"} {
		check(filepath.Join(root, path))
	}
	cfg := filepath.Join(root, "config.json")
	if err := saveConfig(cfg, a.snapshot()); err != nil {
		t.Fatal(err)
	}
	os.Chown(cfg, 65534, 65534)
	if err := saveConfig(cfg, a.snapshot()); err != nil {
		t.Fatal(err)
	}
	check(cfg)
	logger := newDailyLogger(root, cfg)
	logger.write(map[string]any{"event": "管理员测试"})
	check(filepath.Join(root, time.Now().Format("2006-01-02")+".log"))
}
