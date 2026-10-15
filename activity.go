package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type dailyLogger struct {
	mu                         sync.Mutex
	dir, configPath, lastError string
	now                        func() time.Time
}

func newDailyLogger(dir, configPath string) *dailyLogger {
	return &dailyLogger{dir: dir, configPath: configPath, now: time.Now}
}
func (l *dailyLogger) write(row map[string]any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	row["time"] = now.Format("2006-01-02 15:04:05")
	path := filepath.Join(l.dir, now.Format("2006-01-02")+".log")
	err := func() error {
		if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("日志目标不是普通文件")
		} else if e != nil && !os.IsNotExist(e) {
			return e
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		if e = f.Chmod(0600); e != nil {
			return e
		}
		if l.configPath != "" {
			if e = preserveFileOwner(f.Name(), l.configPath); e != nil {
				return e
			}
		}
		return json.NewEncoder(f).Encode(row)
	}()
	if err != nil {
		l.lastError = err.Error()
		log.Print("日志写入失败：", err)
	} else {
		l.lastError = ""
	}
}
func (l *dailyLogger) errorText() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastError
}

type connectionInfo struct {
	Status         string `json:"status"`
	LastSeen       string `json:"last_seen"`
	Client         string `json:"client"`
	Remote         string `json:"remote"`
	ActiveRequests int    `json:"active_requests"`
}
type activityTracker struct {
	mu             sync.Mutex
	lastSeen       time.Time
	client, remote string
	active         int
}

func (a *Agent) beginRequest(remote string) func() {
	a.activity.mu.Lock()
	a.activity.lastSeen = time.Now()
	a.activity.remote = remote
	a.activity.active++
	a.activity.mu.Unlock()
	return func() {
		a.activity.mu.Lock()
		a.activity.active--
		a.activity.lastSeen = time.Now()
		a.activity.mu.Unlock()
	}
}
func (a *Agent) connectionState() connectionInfo {
	a.activity.mu.Lock()
	defer a.activity.mu.Unlock()
	s := connectionInfo{Status: "waiting", Client: a.activity.client, Remote: a.activity.remote, ActiveRequests: a.activity.active}
	if !a.activity.lastSeen.IsZero() {
		s.LastSeen = a.activity.lastSeen.Format("2006-01-02 15:04:05")
		s.Status = "idle"
		if time.Since(a.activity.lastSeen) < 90*time.Second {
			s.Status = "recent"
		}
	}
	if s.ActiveRequests > 0 {
		s.Status = "active"
	}
	return s
}
func redactArgs(args []string) []string {
	out := append([]string{}, args...)
	hideNext := false
	for i, v := range out {
		if hideNext {
			out[i] = "<已隐藏>"
			hideNext = false
			continue
		}
		key := strings.ToLower(strings.SplitN(v, "=", 2)[0])
		if strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "secret") || strings.Contains(key, "api-key") || strings.Contains(key, "api_key") {
			if strings.Contains(v, "=") {
				out[i] = strings.SplitN(v, "=", 2)[0] + "=<已隐藏>"
			} else {
				hideNext = true
			}
		}
	}
	return out
}
func (a *Agent) auditTool(name string, raw json.RawMessage, result any, err error) {
	var args toolArgs
	json.Unmarshal(raw, &args)
	actions := map[string]string{"delete_file": "移入废纸篓/回收站", "read_file": "阅读文件", "write_file": "编辑文件", "list_roots": "查看授权目录", "list_directory": "查看目录", "create_directory": "创建目录", "search_text": "搜索文本", "run_command": "执行命令", "edit_file": "修改文件片段", "move_file": "移动文件", "find_files": "查找文件"}
	row := map[string]any{"action": actions[name], "event": "操作完成", "tool": name, "root": args.Root, "path": args.Path, "result": "成功"}
	if err != nil {
		row["result"] = "失败"
		row["error"] = err.Error()
	}
	if name == "delete_file" {
		row["trash_result"] = result
	}
	if name == "move_file" {
		row["destination"] = args.Destination
	}
	if name == "run_command" {
		row["program"] = args.Program
		row["args"] = redactArgs(args.Args)
		row["execution"] = result
		if value, ok := result.(map[string]any); ok {
			if value["exit_code"] != 0 || value["timed_out"] == true {
				row["result"] = "失败"
			}
		}
	}
	a.audit.write(row)
}
func permissionInstructions() string {
	return "当前权限：" + privilegeMode() + "。这些工具在用户本地电脑运行；先调用 list_roots。服务器自带工具仍在服务器运行。macOS 操作权限不足时，请提示用户先关闭普通连接器，再在本机运行启动-sudo.command 并输入系统密码，然后重连。禁止通过远程命令请求密码或自行提权。sudo 也不能绕过 macOS 隐私权限和系统完整性保护。"
}

func (a *Agent) logDirectory() string {
	if a.audit == nil {
		return ""
	}
	return a.audit.dir
}
