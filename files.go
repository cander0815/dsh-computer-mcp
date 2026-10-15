package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxFile = 1024 * 1024

type toolArgs struct {
	Root        string   `json:"root"`
	Path        string   `json:"path"`
	Text        string   `json:"text"`
	Expected    string   `json:"expected_sha256"`
	Query       string   `json:"query"`
	Program     string   `json:"program"`
	Args        []string `json:"args"`
	Timeout     int      `json:"timeout_seconds"`
	Offset      int      `json:"offset"`
	Limit       int      `json:"limit"`
	OldString   string   `json:"old_string"`
	NewString   string   `json:"new_string"`
	Destination string   `json:"destination"`
	Pattern     string   `json:"pattern"`
	Include     string   `json:"include"`
	Regex       bool     `json:"regex"`
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func readText(path string) ([]byte, error) {
	// 打开前拒绝特殊文件，避免 FIFO 在 open 阶段阻塞。
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("只支持普通文本文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s, err := f.Stat()
	if err != nil || !s.Mode().IsRegular() {
		return nil, errors.New("只支持普通文本文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, errors.New("文件超过 1 MiB，请先拆分文件")
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return nil, errors.New("文件不是 UTF-8 文本")
	}
	return b, nil
}

// sliceLines 按行截取文本：offset 是起始行（从 1 开始，0 表示从头），limit 是行数（0 表示到结尾）。
// 每行保留原来的换行符，截出来的内容和文件里一字不差。
func sliceLines(text string, offset, limit int) (part string, start, end, total int, err error) {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total = len(lines)
	if offset < 0 || limit < 0 {
		return "", 0, 0, total, errors.New("offset 和 limit 不能为负数")
	}
	if offset == 0 {
		offset = 1
	}
	// 空文件只允许从第 1 行读（返回空内容）。
	if offset > max(total, 1) {
		return "", 0, 0, total, fmt.Errorf("offset 超出文件行数（共 %d 行）", total)
	}
	end = total
	if limit > 0 && offset-1+limit < total {
		end = offset - 1 + limit
	}
	if total == 0 {
		return "", 1, 0, 0, nil
	}
	return strings.Join(lines[offset-1:end], ""), offset, end, total, nil
}

func (a *Agent) callTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "list_roots" {
		roots := []any{}
		for _, r := range a.snapshot().Roots {
			roots = append(roots, map[string]any{"id": rootID(r), "name": filepath.Base(r), "path": r})
		}
		return map[string]any{"device": a.snapshot().Name, "roots": roots, "command_execution_enabled": a.snapshot().AllowExec, "privilege": privilegeMode(), "permission_instructions": permissionInstructions()}, nil
	}
	var args toolArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, errors.New("工具参数无效")
	}
	if args.Path == "" {
		args.Path = "."
	}
	switch name {
	case "read_file":
		p, err := a.resolve(args.Root, args.Path, false)
		if err != nil {
			return nil, err
		}
		b, err := readText(p)
		if err != nil {
			return nil, err
		}
		text, start, end, total, err := sliceLines(string(b), args.Offset, args.Limit)
		if err != nil {
			return nil, err
		}
		// sha256 始终是整个文件的哈希，分段读取后也能直接用于 write_file / edit_file。
		return map[string]any{"text": text, "sha256": digest(b), "start_line": start, "end_line": end, "total_lines": total}, nil
	case "list_directory":
		p, err := a.resolve(args.Root, args.Path, false)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for i, e := range entries {
			if i >= 1000 {
				break
			}
			out = append(out, map[string]any{"name": e.Name(), "directory": e.IsDir(), "symlink": e.Type()&os.ModeSymlink != 0})
		}
		return map[string]any{"entries": out, "truncated": len(entries) > 1000}, nil
	case "delete_file":
		return a.deleteFile(ctx, args)
	case "write_file":
		return a.writeFile(args)
	case "edit_file":
		return a.editFile(args)
	case "move_file":
		return a.moveFile(args)
	case "create_directory":
		p, err := a.resolve(args.Root, args.Path, true)
		if err != nil {
			return nil, err
		}
		if err = makeParents(p); err != nil {
			return nil, err
		}
		if s, err := os.Lstat(p); err != nil || !s.IsDir() {
			return nil, errors.New("同名文件已存在，无法创建文件夹")
		}
		return map[string]any{"path": args.Path}, nil
	case "search_text":
		return a.search(ctx, args)
	case "find_files":
		return a.findFiles(ctx, args)
	case "run_command":
		return a.runCommand(ctx, args)
	default:
		return nil, errors.New("未知工具")
	}
}
func (a *Agent) writeFile(args toolArgs) (any, error) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if len(args.Text) > maxFile || !utf8.ValidString(args.Text) {
		return nil, errors.New("文本必须为 UTF-8，且不超过 1 MiB")
	}
	p, err := a.resolve(args.Root, args.Path, true)
	if err != nil {
		return nil, err
	}
	old, err := readText(p)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if exists && (len(old) > maxFile || args.Expected != digest(old)) {
		return nil, errors.New("文件已存在或被修改，请重新读取 SHA-256 后写入")
	}
	if !exists && args.Expected != "new" {
		return nil, errors.New("新建文件必须传 expected_sha256=new")
	}
	if err = commitText(p, args.Text, exists, args.Expected); err != nil {
		return nil, err
	}
	return map[string]any{"sha256": digest([]byte(args.Text)), "bytes": len(args.Text)}, nil
}

// commitText 先写临时文件再整体替换，写到一半出错也不会留下残缺的原文件。
// exists 和 expected 是调用方读到的旧状态：替换前再核对一次，期间文件被别人改过就放弃。
// 调用方必须持有 filesMu。
func commitText(p, text string, exists bool, expected string) error {
	if err := makeParents(filepath.Dir(p)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".dsh-write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	mode := os.FileMode(0600)
	if exists {
		if s, e := os.Stat(p); e == nil {
			mode = s.Mode().Perm()
		}
	}
	if err = f.Chmod(mode); err == nil {
		_, err = f.WriteString(text)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	now, e := readText(p)
	if (exists && (e != nil || digest(now) != expected)) || (!exists && !os.IsNotExist(e)) {
		return errors.New("写入前发现文件变化，请重新读取")
	}
	ownerSource := filepath.Dir(p)
	if exists {
		ownerSource = p
	}
	if err = preserveFileOwner(tmp, ownerSource); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// skippedDirs 是搜索和查找时默认跳过的目录：依赖、虚拟环境和构建产物，文件多且很少需要看。
// build 不在其中：不少项目在 build/ 下放构建脚本和源码，跳过会让模型以为代码不存在。
// 把 path 直接指到这些目录里面时仍会搜索。
var skippedDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true, "dist": true, "target": true, ".next": true}

// walkFiles 遍历 start 下的普通文件，跳过符号链接和 skippedDirs。
// 回调拿到的 rel 是相对授权根目录的路径（统一用 /），可以直接交给其它工具；回调返回 fs.SkipAll 可提前结束。
// 返回实际跳过的目录（最多 20 个），放进结果里，模型就知道要找的内容可能在哪些目录里。
func walkFiles(ctx context.Context, start, startRel string, visit func(path, rel string, d fs.DirEntry) error) ([]string, error) {
	skipped := []string{}
	relative := func(path string) string {
		inner, _ := filepath.Rel(start, path)
		return filepath.ToSlash(filepath.Join(startRel, inner))
	}
	err := filepath.WalkDir(start, func(path string, d fs.DirEntry, e error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e != nil || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if path != start && skippedDirs[d.Name()] {
				if len(skipped) < 20 {
					skipped = append(skipped, relative(path))
				}
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return visit(path, relative(path), d)
	})
	return skipped, err
}
func (a *Agent) search(ctx context.Context, args toolArgs) (any, error) {
	if args.Query == "" || len(args.Query) > 1000 {
		return nil, errors.New("请输入不超过 1000 字节的搜索文本")
	}
	match := func(line string) bool { return strings.Contains(line, args.Query) }
	if args.Regex {
		// Go 的正则引擎是线性时间的，不会被恶意表达式拖死。
		re, err := regexp.Compile(args.Query)
		if err != nil {
			return nil, fmt.Errorf("正则表达式无效：%v", err)
		}
		match = re.MatchString
	}
	include, err := namePattern(args.Include)
	if err != nil {
		return nil, err
	}
	p, err := a.resolve(args.Root, args.Path, false)
	if err != nil {
		return nil, err
	}
	out := []any{}
	// scanned 只统计成功读成文本的文件；attempted 限制总读取次数，避免大量图片等二进制文件拖慢搜索。
	scanned, attempted := 0, 0
	truncated := false
	skipped, err := walkFiles(ctx, p, filepath.Clean(args.Path), func(path, rel string, d fs.DirEntry) error {
		if include != "" {
			if ok, err := matchName(include, d.Name()); err != nil || !ok {
				return err
			}
		}
		if scanned >= 1000 || attempted >= 5000 {
			truncated = true
			return fs.SkipAll
		}
		attempted++
		b, e := readText(path)
		if e != nil {
			return nil
		}
		scanned++
		for i, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if match(line) {
				if len(line) > 1000 {
					line = line[:1000]
				}
				out = append(out, map[string]any{"path": rel, "line": i + 1, "text": line})
				if len(out) >= 100 {
					truncated = true
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	return map[string]any{"matches": out, "scanned_files": scanned, "truncated": truncated, "skipped_dirs": skipped}, err
}
