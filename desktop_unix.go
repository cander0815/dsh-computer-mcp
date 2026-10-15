//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func privilegeMode() string {
	if os.Geteuid() == 0 {
		return "管理员（root）"
	}
	return "普通用户"
}
func preserveFileOwner(target, source string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	return os.Chown(target, int(stat.Uid), int(stat.Gid))
}
func browserCommand(url string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		if os.Geteuid() == 0 && os.Getenv("SUDO_USER") != "" {
			return exec.Command("sudo", "-u", os.Getenv("SUDO_USER"), "open", url)
		}
		return exec.Command("open", url)
	}
	return exec.Command("xdg-open", url)
}
