//go:build windows

package main

import "os/exec"

func privilegeMode() string                         { return "Windows 当前用户（未检测管理员权限）" }
func preserveFileOwner(target, source string) error { return nil }
func browserCommand(url string) *exec.Cmd {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
}
