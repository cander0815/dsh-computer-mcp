package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type cappedOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := 65536 - len(b.data)
	if n > left {
		b.truncated = true
		p = p[:left]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func safeEnvironment() []string {
	out := []string{}
	for _, value := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(value, "=", 2)[0])
		if strings.Contains(key, "KEY") || strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET") || strings.Contains(key, "PASSWORD") || strings.HasPrefix(key, "DSH_") {
			continue
		}
		out = append(out, value)
	}
	return out
}
func (a *Agent) runCommand(ctx context.Context, args toolArgs) (any, error) {
	if !a.snapshot().AllowExec {
		return nil, errors.New("命令执行未开启，请在本地配置页开启")
	}
	if args.Program == "" || len(args.Program) > 2048 || len(args.Args) > 100 {
		return nil, errors.New("命令参数无效")
	}
	for _, v := range args.Args {
		if len(v) > 8192 {
			return nil, errors.New("单个参数过长")
		}
	}
	cwd, err := a.resolve(args.Root, args.Path, false)
	if err != nil {
		return nil, err
	}
	s, err := os.Stat(cwd)
	if err != nil || !s.IsDir() {
		return nil, errors.New("命令工作目录必须是文件夹")
	}
	seconds := args.Timeout
	if seconds == 0 {
		seconds = 30
	}
	if seconds < 1 || seconds > 120 {
		return nil, errors.New("命令超时必须在 1 到 120 秒之间")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args.Program, args.Args...)
	cmd.Dir = cwd
	cmd.Env = safeEnvironment()
	cmd.WaitDelay = 2 * time.Second
	prepareProcess(cmd)
	out := &cappedOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	err = cmd.Run()
	cleanupProcess(cmd)
	exit := 0
	if err != nil {
		exit = -1
		if e, ok := err.(*exec.ExitError); ok {
			exit = e.ExitCode()
		} else if ctx.Err() == nil {
			return nil, err
		}
	}
	return map[string]any{"output": strings.ToValidUTF8(string(out.data), "�"), "exit_code": exit, "timed_out": ctx.Err() != nil, "truncated": out.truncated}, nil
}
