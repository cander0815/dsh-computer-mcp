package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// makeParents 逐级创建缺少的目录（类似 mkdir -p）。
// 每一级新目录沿用上一级的所有者，sudo 运行时也不会变成 root 所有。
func makeParents(dir string) error {
	if _, err := os.Lstat(dir); err == nil || !os.IsNotExist(err) {
		return err
	}
	if err := makeParents(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return preserveFileOwner(dir, filepath.Dir(dir))
}

// editFile 把文件里唯一出现的一段 old_string 换成 new_string，模型改一处不必重发整个文件。
// old_string 必须恰好出现一次：找不到说明模型记错了原文，出现多次则不知道该改哪一处，两种情况都拒绝。
func (a *Agent) editFile(args toolArgs) (any, error) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if args.OldString == "" {
		return nil, errors.New("old_string 不能为空；新建或整体重写请用 write_file")
	}
	if args.OldString == args.NewString {
		return nil, errors.New("old_string 和 new_string 相同，无需修改")
	}
	p, err := a.resolve(args.Root, args.Path, false)
	if err != nil {
		return nil, err
	}
	old, err := readText(p)
	if err != nil {
		return nil, err
	}
	if args.Expected != "" && args.Expected != digest(old) {
		return nil, errors.New("文件已被修改，请重新读取后再编辑")
	}
	content := string(old)
	from, to := args.OldString, args.NewString
	// Windows 文件用 \r\n 换行时，新内容也统一成 \r\n，避免同一个文件里换行混杂。
	crlf := strings.Contains(content, "\r\n")
	if crlf {
		to = strings.ReplaceAll(strings.ReplaceAll(to, "\r\n", "\n"), "\n", "\r\n")
	}
	i := strings.Index(content, from)
	// 模型给的原文通常只用 \n，在 \r\n 文件里找不到时，换成 \r\n 再找一次。
	if i < 0 && crlf && !strings.Contains(from, "\r\n") {
		from = strings.ReplaceAll(from, "\n", "\r\n")
		i = strings.Index(content, from)
	}
	if i < 0 {
		return nil, errors.New("没有找到 old_string，请重新读取文件，按原文（含缩进和空格）复制")
	}
	// 从第一处的下一个字符接着找，互相重叠的出现也能发现（strings.Count 只数不重叠的）。
	if strings.Contains(content[i+1:], from) {
		return nil, fmt.Errorf("old_string 在文件中出现了不止一次（至少 %d 次），请多带几行上下文，让它只出现一次", max(strings.Count(content, from), 2))
	}
	text := content[:i] + to + content[i+len(from):]
	// 和 readText 的规则保持一致，否则写进去之后自己就读不回来了。
	if len(text) > maxFile || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return nil, errors.New("编辑后的文本必须为 UTF-8、不含 NUL 字符，且不超过 1 MiB")
	}
	if err = commitText(p, text, true, digest(old)); err != nil {
		return nil, err
	}
	return map[string]any{"sha256": digest([]byte(text)), "bytes": len(text)}, nil
}

// moveFile 在同一个授权目录内移动或重命名文件、文件夹。目标已存在时拒绝，绝不覆盖。
func (a *Agent) moveFile(args toolArgs) (any, error) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if filepath.Clean(args.Path) == "." {
		return nil, errors.New("不能移动授权目录本身")
	}
	from, err := a.resolve(args.Root, args.Path, false)
	if err != nil {
		return nil, err
	}
	to, err := a.resolve(args.Root, args.Destination, true)
	if err != nil {
		return nil, err
	}
	source, err := os.Lstat(from)
	if err != nil {
		return nil, err
	}
	if target, err := os.Lstat(to); err == nil {
		// macOS、Windows 默认不区分大小写：a.txt 改名为 A.txt 时两边是同一个文件，这种情况允许。
		caseOnlyRename := os.SameFile(source, target) && strings.EqualFold(from, to) && from != to
		if !caseOnlyRename {
			return nil, errors.New("目标已存在，move_file 不会覆盖；请换一个目标路径")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// 系统本来就会拒绝，这里提前拦下，免得先建好的上级目录留在原地。
	if strings.HasPrefix(to, from+string(filepath.Separator)) {
		return nil, errors.New("不能把文件夹移到它自己里面")
	}
	if err = makeParents(filepath.Dir(to)); err != nil {
		return nil, err
	}
	if err = os.Rename(from, to); err != nil {
		return nil, err
	}
	return map[string]any{"path": args.Path, "destination": args.Destination}, nil
}

var errBadPattern = errors.New("通配符无效：只匹配文件名，例如 *.go；不支持 /、** 和 {a,b}，要限定目录请用 path")

// namePattern 整理文件名通配符。这里只拿文件名去比对：
// 开头的 **/ 去掉即可（本来就会逐层查找）；其余含 / 或 {} 的写法 Go 不支持，直接报错，免得静默返回空结果。
func namePattern(pattern string) (string, error) {
	pattern = strings.TrimPrefix(pattern, "**/")
	if strings.ContainsAny(pattern, "/{}") || strings.ContainsRune(pattern, filepath.Separator) {
		return "", errBadPattern
	}
	// filepath.Match 只有比对走到坏掉的那一段才会报错，所以拿空串和模式本身各试一次：
	// 用模式本身当文件名时字面部分都能对上，比对能走到后面的段。漏网的由遍历时的 matchName 兜底。
	for _, probe := range []string{"", pattern} {
		if _, err := filepath.Match(pattern, probe); err != nil {
			return "", errBadPattern
		}
	}
	return pattern, nil
}

// matchName 比对文件名。filepath.Match 有些坏模式要比对到一半才发现，所以遍历中也要检查错误。
func matchName(pattern, name string) (bool, error) {
	ok, err := filepath.Match(pattern, name)
	if err != nil {
		return false, errBadPattern
	}
	return ok, nil
}

// findFiles 按文件名通配符（如 *.go、*_test.py）查找文件，返回相对授权根目录的路径。
func (a *Agent) findFiles(ctx context.Context, args toolArgs) (any, error) {
	pattern, err := namePattern(args.Pattern)
	if err != nil || pattern == "" {
		return nil, errBadPattern
	}
	p, err := a.resolve(args.Root, args.Path, false)
	if err != nil {
		return nil, err
	}
	files := []string{}
	visited := 0
	truncated := false
	skipped, err := walkFiles(ctx, p, filepath.Clean(args.Path), func(path, rel string, d fs.DirEntry) error {
		visited++
		if visited > 50000 {
			truncated = true
			return fs.SkipAll
		}
		ok, err := matchName(pattern, d.Name())
		if err != nil || !ok {
			return err
		}
		// 已经收满 200 个又遇到新的匹配，才算真的有更多结果没返回。
		if len(files) >= 200 {
			truncated = true
			return fs.SkipAll
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"files": files, "truncated": truncated, "skipped_dirs": skipped}, nil
}
