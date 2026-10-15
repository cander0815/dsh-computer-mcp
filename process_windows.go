//go:build windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func shutdownSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }

func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		kill.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

// Windows 超时由 taskkill 清理仍存活的进程树，不对已退出的 PID 再发出终止请求。
func cleanupProcess(cmd *exec.Cmd) {}
