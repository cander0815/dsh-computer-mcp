package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"time"
	"unicode/utf16"
)

//go:embed trash_windows.cs
var windowsTrashSource string

func encodedPowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, 2*len(units))
	for i, v := range units {
		binary.LittleEndian.PutUint16(data[2*i:], v)
	}
	return base64.StdEncoding.EncodeToString(data)
}
func systemTrash(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	script := "$ErrorActionPreference='Stop'\n[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)\nAdd-Type -TypeDefinition @'\n" + windowsTrashSource + "\n'@\n[SafeTrash]::Recycle($env:DSH_TRASH_PATH)\n[Console]::WriteLine('recycled')"
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Sta", "-EncodedCommand", encodedPowerShell(script))
	cmd.Env = append(safeEnvironment(), "DSH_TRASH_PATH="+path)
	prepareProcess(cmd)
	defer cleanupProcess(cmd)
	output := &cappedOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("Windows 回收站操作失败：%s；%w", string(output.data), err)
	}
	if string(output.data) != "recycled\r\n" && string(output.data) != "recycled\n" {
		return "", fmt.Errorf("Windows 未确认文件已移入回收站，请检查原文件与回收站")
	}
	return "Windows 回收站", nil
}
