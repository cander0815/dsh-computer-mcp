package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// 使用 Foundation 文件 API，不控制 Finder，不调用永久删除接口。
const macTrashScript = `ObjC.import('Foundation');
function run(argv) {
 // 不读取可选的输出指针，避免旧版 JXA 对 NSURL 输出参数的桥接差异。
 var ok=$.NSFileManager.defaultManager.trashItemAtURLResultingItemURLError($.NSURL.fileURLWithPath(argv[0]),null,null);
 if(!ok) throw new Error('系统拒绝移入废纸篓，请检查权限、磁盘与废纸篓状态');
 return JSON.stringify({destination:'macOS 废纸篓'});
}`

func systemTrash(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	argv := []string{"-l", "JavaScript", "-e", macTrashScript, path}
	program := "/usr/bin/osascript"
	if os.Geteuid() == 0 {
		user := os.Getenv("SUDO_USER")
		if user == "" || user == "root" {
			return "", fmt.Errorf("请使用原用户的启动.command 或启动-sudo.command，以便正确使用该用户的废纸篓")
		}
		argv = append([]string{"-n", "-u", user, program}, argv...)
		program = "/usr/bin/sudo"
	}
	cmd := exec.CommandContext(ctx, program, argv...)
	prepareProcess(cmd)
	defer cleanupProcess(cmd)
	out := &cappedOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("系统废纸篓操作失败：%s；%w", string(out.data), err)
	}
	var result struct {
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal(out.data, &result); err != nil || result.Destination == "" {
		return "", fmt.Errorf("系统未返回有效废纸篓位置，请检查原文件与废纸篓后再操作")
	}
	return result.Destination, nil
}
