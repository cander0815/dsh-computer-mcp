package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const version = "0.0.1"

type Config struct {
	Name      string   `json:"name"`
	Token     string   `json:"token"`
	Roots     []string `json:"roots"`
	AllowExec bool     `json:"allow_exec"`
}
type Agent struct {
	trash      func(context.Context, string) (string, error)
	audit      *dailyLogger
	activity   activityTracker
	mu         sync.RWMutex
	filesMu    sync.Mutex
	cfg        Config
	configPath string
	adminToken string
	port       int
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func rootID(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:6])
}
func validateConfig(c Config) (Config, error) {
	if len(c.Token) < 32 {
		return c, errors.New("连接令牌至少需要 32 个字符")
	}
	if len(c.Name) > 200 {
		return c, errors.New("设备名称过长")
	}
	if len(c.Roots) > 20 {
		return c, errors.New("最多允许 20 个目录")
	}
	roots := []string{}
	seen := map[string]bool{}
	for _, root := range c.Roots {
		if !filepath.IsAbs(root) {
			return c, errors.New("目录必须是绝对路径")
		}
		p, err := filepath.EvalSymlinks(root)
		if err != nil {
			return c, err
		}
		s, err := os.Stat(p)
		if err != nil || !s.IsDir() {
			return c, errors.New("请选择已存在的文件夹")
		}
		p = filepath.Clean(p)
		if !seen[p] {
			roots = append(roots, p)
			seen[p] = true
		}
	}
	c.Roots = roots
	return c, nil
}
func newAgent(c Config) (*Agent, error) {
	c, err := validateConfig(c)
	if err != nil {
		return nil, err
	}
	return &Agent{cfg: c, adminToken: randomToken(), port: 18790}, nil
}
func (a *Agent) snapshot() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	c := a.cfg
	c.Roots = append([]string{}, c.Roots...)
	return c
}
func (a *Agent) update(c Config) error {
	c, err := validateConfig(c)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.configPath != "" {
		if err = saveConfig(a.configPath, c); err != nil {
			return err
		}
	}
	a.cfg = c
	return nil
}
func saveConfig(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if _, e := os.Stat(path); e == nil {
		if e = preserveFileOwner(name, path); e != nil {
			return e
		}
	}
	return os.Rename(name, path)
}
func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		name, _ := os.Hostname()
		c := Config{Name: name, Token: randomToken(), Roots: []string{}}
		return c, saveConfig(path, c)
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return validateConfig(c)
}
func (a *Agent) resolve(id, relative string, create bool) (string, error) {
	var root string
	for _, r := range a.snapshot().Roots {
		if rootID(r) == id {
			root = r
			break
		}
	}
	if root == "" {
		return "", errors.New("未授权的目录，请先调用 list_roots")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(canonical) != root {
		return "", errors.New("授权目录已移动或被替换，请重新选择目录")
	}
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", errors.New("文件路径必须相对于授权目录")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("路径超出了授权目录")
	}
	current := root
	// 不跟随任何子路径符号链接，避免链接指向授权目录外。
	parts := strings.Split(clean, string(filepath.Separator))
	for i, part := range parts {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		s, err := os.Lstat(current)
		if os.IsNotExist(err) && create {
			// 从这一段开始都还不存在，自然也不会有符号链接；缺少的父目录由调用方用 makeParents 创建。
			// 把剩下还没检查的段拼回去，得到完整的目标路径。
			rest := filepath.Join(parts[i+1:]...)
			return filepath.Join(current, rest), nil
		}
		if err != nil {
			return "", err
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("不允许通过符号链接访问文件")
		}
	}
	return current, nil
}
