package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decode 把工具返回值转成通用 map，方便断言字段。
func decode(t *testing.T, value any) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(mustJSON(value), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEditFileReplacesOnlyUniqueText(t *testing.T) {
	a, root := fixture(t)
	p := filepath.Join(root, "a.go")
	os.WriteFile(p, []byte("one\ntwo\nthree\ntwo\n"), 0644)
	id := rootID(root)
	if _, err := invoke(a, "edit_file", map[string]any{"root": id, "path": "a.go", "old_string": "two", "new_string": "2"}); err == nil || !strings.Contains(err.Error(), "2 次") {
		t.Fatal("出现多次时应拒绝并说明次数：", err)
	}
	if _, err := invoke(a, "edit_file", map[string]any{"root": id, "path": "a.go", "old_string": "four", "new_string": "4"}); err == nil {
		t.Fatal("找不到原文时应拒绝")
	}
	value, err := invoke(a, "edit_file", map[string]any{"root": id, "path": "a.go", "old_string": "one\ntwo", "new_string": "1\n2"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "1\n2\nthree\ntwo\n" {
		t.Fatalf("替换结果不对：%q", b)
	}
	if decode(t, value)["sha256"] != digest(b) {
		t.Fatal("应返回新内容的哈希，便于连续编辑")
	}
	if info, _ := os.Stat(p); info.Mode().Perm() != 0644 {
		t.Fatal("编辑后不应改变文件权限")
	}
	if _, err := invoke(a, "edit_file", map[string]any{"root": id, "path": "a.go", "old_string": "three", "new_string": "3", "expected_sha256": digest([]byte("旧内容"))}); err == nil {
		t.Fatal("传了过期哈希时应拒绝")
	}
}

func TestEditFileMatchesWindowsLineEndings(t *testing.T) {
	a, root := fixture(t)
	p := filepath.Join(root, "win.txt")
	os.WriteFile(p, []byte("a\r\nb\r\nc\r\n"), 0644)
	if _, err := invoke(a, "edit_file", map[string]any{"root": rootID(root), "path": "win.txt", "old_string": "a\nb", "new_string": "x\ny"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "x\r\ny\r\nc\r\n" {
		t.Fatalf("应保留 CRLF：%q", b)
	}
}

func TestReadFileByLines(t *testing.T) {
	a, root := fixture(t)
	content := "l1\nl2\nl3\nl4\n"
	os.WriteFile(filepath.Join(root, "a.txt"), []byte(content), 0644)
	value, err := invoke(a, "read_file", map[string]any{"root": rootID(root), "path": "a.txt", "offset": 2, "limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, value)
	if got["text"] != "l2\nl3\n" || got["start_line"] != 2.0 || got["end_line"] != 3.0 || got["total_lines"] != 4.0 {
		t.Fatalf("分段读取不对：%v", got)
	}
	if got["sha256"] != digest([]byte(content)) {
		t.Fatal("分段读取也应返回整个文件的哈希")
	}
	if _, err := invoke(a, "read_file", map[string]any{"root": rootID(root), "path": "a.txt", "offset": 9}); err == nil {
		t.Fatal("起始行超出文件时应报错")
	}
}

func TestWriteAndCreateDirectoryMakeParents(t *testing.T) {
	a, root := fixture(t)
	id := rootID(root)
	if _, err := invoke(a, "write_file", map[string]any{"root": id, "path": "src/a/b/c.txt", "text": "深层", "expected_sha256": "new"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src", "a", "b", "c.txt")); string(b) != "深层" {
		t.Fatal("应自动创建缺少的父目录")
	}
	if _, err := invoke(a, "create_directory", map[string]any{"root": id, "path": "x/y/z"}); err != nil {
		t.Fatal(err)
	}
	if s, err := os.Stat(filepath.Join(root, "x", "y", "z")); err != nil || !s.IsDir() {
		t.Fatal("create_directory 应补齐上级目录")
	}
	if _, err := invoke(a, "write_file", map[string]any{"root": id, "path": "../out/a.txt", "text": "坏", "expected_sha256": "new"}); err == nil {
		t.Fatal("补目录不能越界")
	}
}

func TestMoveFile(t *testing.T) {
	a, root := fixture(t)
	id := rootID(root)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("A"), 0644)
	os.WriteFile(filepath.Join(root, "keep.txt"), []byte("K"), 0644)
	if _, err := invoke(a, "move_file", map[string]any{"root": id, "path": "a.txt", "destination": "keep.txt"}); err == nil {
		t.Fatal("目标已存在时不应覆盖")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "keep.txt")); string(b) != "K" {
		t.Fatal("已存在的目标被改动了")
	}
	if _, err := invoke(a, "move_file", map[string]any{"root": id, "path": "a.txt", "destination": "new/dir/b.txt"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "new", "dir", "b.txt")); string(b) != "A" {
		t.Fatal("移动后内容不对")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("原文件应已移走")
	}
	for _, c := range [][2]string{{".", "x"}, {"keep.txt", "../outside.txt"}, {"../x", "y"}} {
		if _, err := invoke(a, "move_file", map[string]any{"root": id, "path": c[0], "destination": c[1]}); err == nil {
			t.Fatal("接受了危险的移动：", c)
		}
	}
}

func TestSearchAndFindUseRootRelativePaths(t *testing.T) {
	a, root := fixture(t)
	id := rootID(root)
	os.MkdirAll(filepath.Join(root, "src", "lib"), 0755)
	os.MkdirAll(filepath.Join(root, ".venv"), 0755)
	os.WriteFile(filepath.Join(root, "src", "lib", "a.go"), []byte("func Hello() {}\n"), 0644)
	os.WriteFile(filepath.Join(root, "src", "lib", "a.md"), []byte("Hello\n"), 0644)
	os.WriteFile(filepath.Join(root, ".venv", "x.go"), []byte("func Hello() {}\n"), 0644)

	value, err := invoke(a, "search_text", map[string]any{"root": id, "path": "src", "query": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	matches := decode(t, value)["matches"].([]any)
	if len(matches) != 2 {
		t.Fatalf("应搜到 src 下 2 处：%v", matches)
	}
	first := matches[0].(map[string]any)["path"].(string)
	if !strings.HasPrefix(first, "src/lib/") {
		t.Fatal("搜索结果路径应相对授权根目录：", first)
	}
	// 用搜索结果的路径直接读取，必须能读到同一个文件。
	if _, err := invoke(a, "read_file", map[string]any{"root": id, "path": first}); err != nil {
		t.Fatal(err)
	}

	value, _ = invoke(a, "search_text", map[string]any{"root": id, "query": `func \w+\(`, "regex": true, "include": "*.go"})
	matches = decode(t, value)["matches"].([]any)
	if len(matches) != 1 || matches[0].(map[string]any)["path"] != "src/lib/a.go" {
		t.Fatalf("正则 + include 过滤，且应跳过 .venv：%v", matches)
	}
	if _, err := invoke(a, "search_text", map[string]any{"root": id, "query": "(", "regex": true}); err == nil {
		t.Fatal("无效正则应报错")
	}

	value, err = invoke(a, "find_files", map[string]any{"root": id, "pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	files := decode(t, value)["files"].([]any)
	if len(files) != 1 || files[0] != "src/lib/a.go" {
		t.Fatalf("按名查找不对：%v", files)
	}
	if _, err := invoke(a, "find_files", map[string]any{"root": id, "pattern": "[bad"}); err == nil {
		t.Fatal("无效通配符应报错")
	}
}

func TestEditFileReviewCases(t *testing.T) {
	a, root := fixture(t)
	id := rootID(root)
	// CRLF 文件里单行原文换成多行新内容，新内容也要用 CRLF。
	os.WriteFile(filepath.Join(root, "win.txt"), []byte("a\r\nb\r\n"), 0644)
	if _, err := invoke(a, "edit_file", map[string]any{"root": id, "path": "win.txt", "old_string": "a", "new_string": "x\ny"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "win.txt")); string(b) != "x\r\ny\r\nb\r\n" {
		t.Fatalf("CRLF 文件混入了 LF：%q", b)
	}
	// 重叠出现也算多处，不能静默改第一处。
	os.WriteFile(filepath.Join(root, "brace.txt"), []byte("}\n}\n}"), 0644)
	if _, err := invoke(a, "edit_file", map[string]any{"root": id, "path": "brace.txt", "old_string": "}\n}", "new_string": "]"}); err == nil {
		t.Fatal("重叠出现时应拒绝")
	}
	if _, err := invoke(a, "edit_file", map[string]any{"root": id, "path": "brace.txt", "old_string": "}\n}\n}", "new_string": "a\x00b"}); err == nil {
		t.Fatal("不应写入 NUL，否则之后读不回来")
	}
}

func TestMoveFileReviewCases(t *testing.T) {
	a, root := fixture(t)
	id := rootID(root)
	os.MkdirAll(filepath.Join(root, "d"), 0755)
	_, err := invoke(a, "move_file", map[string]any{"root": id, "path": "d", "destination": "d/sub/d2"})
	if err == nil || !strings.Contains(err.Error(), "自己里面") {
		t.Fatal("把文件夹移进自己应给出明确提示：", err)
	}
	if _, err := os.Stat(filepath.Join(root, "d", "sub")); !os.IsNotExist(err) {
		t.Fatal("失败的移动不应留下新目录")
	}
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("A"), 0644)
	if _, err := invoke(a, "move_file", map[string]any{"root": id, "path": "a.txt", "destination": "a.txt"}); err == nil {
		t.Fatal("移到自身应拒绝")
	}
}

func TestPatternsAndLimits(t *testing.T) {
	a, root := fixture(t)
	id := rootID(root)
	os.MkdirAll(filepath.Join(root, "src"), 0755)
	os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("x\n"), 0644)
	value, err := invoke(a, "find_files", map[string]any{"root": id, "pattern": "**/*.go"})
	if err != nil || len(decode(t, value)["files"].([]any)) != 1 {
		t.Fatal("**/ 前缀应按文件名匹配：", value, err)
	}
	for _, bad := range []string{"src/*.go", "*.{ts,tsx}", "x*[a-]"} {
		if _, err := invoke(a, "find_files", map[string]any{"root": id, "pattern": bad}); err == nil {
			t.Fatal("不支持的通配符应明确报错：", bad)
		}
		if _, err := invoke(a, "search_text", map[string]any{"root": id, "query": "x", "include": bad}); err == nil {
			t.Fatal("不支持的 include 应明确报错：", bad)
		}
	}
	// 恰好 200 个匹配、后面只有不匹配的文件时，不算截断。
	for i := 0; i < 200; i++ {
		os.WriteFile(filepath.Join(root, "src", fmt.Sprintf("f%03d.txt", i)), nil, 0644)
	}
	os.WriteFile(filepath.Join(root, "src", "zz.md"), nil, 0644)
	value, _ = invoke(a, "find_files", map[string]any{"root": id, "pattern": "*.txt"})
	if got := decode(t, value); len(got["files"].([]any)) != 200 || got["truncated"] != false {
		t.Fatal("恰好 200 个时不应报截断")
	}
	os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0644)
	if _, err := invoke(a, "read_file", map[string]any{"root": id, "path": "empty.txt", "offset": 5}); err == nil {
		t.Fatal("空文件起始行超出也应报错")
	}
}

func TestSkippedDirsAreReported(t *testing.T) {
	a, root := fixture(t)
	id := rootID(root)
	for _, dir := range []string{"build", "target", "src/dist"} {
		os.MkdirAll(filepath.Join(root, dir), 0755)
		os.WriteFile(filepath.Join(root, dir, "a.go"), []byte("needle\n"), 0644)
	}
	value, _ := invoke(a, "find_files", map[string]any{"root": id, "pattern": "*.go"})
	got := decode(t, value)
	if files := got["files"].([]any); len(files) != 1 || files[0] != "build/a.go" {
		t.Fatal("build 不应再被跳过：", files)
	}
	if skipped := fmt.Sprint(got["skipped_dirs"]); skipped != "[src/dist target]" {
		t.Fatal("应列出被跳过的目录：", skipped)
	}
	value, _ = invoke(a, "search_text", map[string]any{"root": id, "query": "needle"})
	if skipped := fmt.Sprint(decode(t, value)["skipped_dirs"]); skipped != "[src/dist target]" {
		t.Fatal("搜索结果也应列出被跳过的目录：", skipped)
	}
}

func TestNewToolsListed(t *testing.T) {
	b := string(mustJSON(toolDefinitions(false)))
	for _, name := range []string{"edit_file", "move_file", "find_files"} {
		if !strings.Contains(b, `"`+name+`"`) {
			t.Fatal("工具列表缺少", name)
		}
	}
}
