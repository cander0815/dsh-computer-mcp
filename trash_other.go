//go:build !darwin && !windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

func systemTrash(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gio", "trash", "--", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("系统回收站不可用（需要 gio 与可用的桌面回收站）：%s；%w", string(out), err)
	}
	return "系统回收站", nil
}
