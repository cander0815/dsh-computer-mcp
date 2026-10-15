package main

import (
	"context"
	"fmt"
	"os"
)

func (a *Agent) deleteFile(ctx context.Context, args toolArgs) (any, error) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	path, err := a.resolve(args.Root, args.Path, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("delete_file 仅支持普通文件，不支持目录、符号链接或特殊文件")
	}
	move := a.trash
	if move == nil {
		move = systemTrash
	}
	destination, err := move(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("移入废纸篓/回收站失败，没有执行永久删除：%w", err)
	}
	return map[string]any{"path": args.Path, "trashed": true, "permanently_deleted": false, "destination": destination, "message": "文件已移入系统废纸篓或回收站。请提醒用户：确认不再需要后，自行清空废纸篓/回收站以释放空间；清空前可尝试恢复。不要自动清空，也不要用命令永久删除作为替代。"}, nil
}
