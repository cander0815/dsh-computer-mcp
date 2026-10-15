package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func sendJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func tokenMatches(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
func localAddresses() []string {
	out := []string{"127.0.0.1"}
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && (ip.IsPrivate() || ip[12] == 100 && ip[13] >= 64 && ip[13] < 128) {
			out = append(out, ip.String())
		}
	}
	return out
}
func validMCPHost(authority string) bool {
	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		host = authority
	}
	for _, ip := range localAddresses() {
		if ip == host {
			return true
		}
	}
	return false
}
func (a *Agent) MCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if !validMCPHost(r.Host) || r.Header.Get("Origin") != "" {
			http.Error(w, "来源不允许", 403)
			return
		}
		if !tokenMatches(r.Header.Get("Authorization"), "Bearer "+a.snapshot().Token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "连接令牌无效", 401)
			return
		}
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "不提供事件流", 405)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "需要 JSON 请求", 415)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2*maxFile)
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JSONRPC != "2.0" {
			sendJSON(w, 400, map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "无效的 JSON-RPC 请求"}})
			return
		}
		if len(req.ID) == 0 {
			if strings.HasPrefix(req.Method, "notifications/") {
				w.WriteHeader(202)
				return
			}
			http.Error(w, "请求缺少 id", 400)
			return
		}
		finish := a.beginRequest(r.RemoteAddr)
		defer finish()
		var result any
		code := 0
		message := ""
		switch req.Method {
		case "initialize":
			var p struct {
				Protocol string `json:"protocolVersion"`
				Client   struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
			}
			json.Unmarshal(req.Params, &p)
			client := p.Client.Name
			if len(client) > 200 {
				client = client[:200]
			}
			a.activity.mu.Lock()
			a.activity.client = client
			a.activity.mu.Unlock()
			a.audit.write(map[string]any{"event": "连接成功", "client": client, "remote": r.RemoteAddr, "privilege": privilegeMode()})
			protocol := p.Protocol
			switch protocol {
			case "2025-03-26", "2025-06-18", "2025-11-25":
			default:
				protocol = "2025-03-26"
			}
			result = map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "dsh-computer-mcp", "version": version}, "instructions": permissionInstructions()}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": toolDefinitions(a.snapshot().AllowExec)}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				code = -32602
				message = "工具参数无效"
				break
			}
			value, err := a.callTool(r.Context(), p.Name, p.Arguments)
			if err != nil && isPermissionError(err) {
				err = fmt.Errorf("%w；%s", err, permissionInstructions())
			}
			a.auditTool(p.Name, p.Arguments, value, err)
			text := ""
			if err != nil {
				text = err.Error()
			} else {
				b, _ := json.Marshal(value)
				text = string(b)
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil}
		default:
			code = -32601
			message = "不支持的方法"
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if code != 0 {
			response["error"] = map[string]any{"code": code, "message": message}
		} else {
			response["result"] = result
		}
		sendJSON(w, 200, response)
	})
}
func toolDefinitions(allowExec bool) []any {
	field := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	lineNumber := func(description string) map[string]any {
		return map[string]any{"type": "integer", "minimum": 0, "description": description}
	}
	skipNote := "跳过 .git、node_modules、.venv、venv、__pycache__、dist、target、.next 和符号链接，实际跳过的目录列在 skipped_dirs；要看这些目录就把 path 指进去。"
	definitions := []struct {
		name, description string
		props             map[string]any
		required          []string
	}{
		{"list_roots", "列出此电脑授权的项目目录及命令执行状态。", map[string]any{}, []string{}},
		{"list_directory", "查看本地项目的目录。path 是相对路径，默认为 .。", map[string]any{"root": field("list_roots 返回的目录 id"), "path": field("相对目录")}, []string{"root"}},
		{"delete_file", "仅把授权目录内的一个普通文件移入系统废纸篓/回收站，不接受目录，不永久删除。失败时禁止改用 rm/Remove-Item 等永久删除。成功后必须提醒用户确认不再需要再自行清空废纸篓/回收站以释放空间；清空前可以恢复，禁止自动清空。", map[string]any{"root": field("目录 id"), "path": field("要移入废纸篓的相对文件路径")}, []string{"root", "path"}},
		{"read_file", "读取本地 UTF-8 文本和整个文件的 SHA-256，不超过 1 MiB。大文件可用 offset、limit 按行分段读取，返回 total_lines。", map[string]any{"root": field("目录 id"), "path": field("相对文件路径"), "offset": lineNumber("起始行，从 1 开始，默认从头"), "limit": lineNumber("读取行数，默认到结尾")}, []string{"root", "path"}},
		{"edit_file", "修改已有文件的一处内容：把 old_string 替换为 new_string。old_string 必须按原文（含缩进和空格）复制，且在文件中只出现一次，否则失败；不唯一时多带几行上下文。改已有文件优先用它，不必重写整个文件。", map[string]any{"root": field("目录 id"), "path": field("相对文件路径"), "old_string": field("要替换的原文"), "new_string": field("替换后的内容"), "expected_sha256": field("可选：最近读取的哈希，文件变了就拒绝")}, []string{"root", "path", "old_string", "new_string"}},
		{"create_directory", "在授权目录中新建文件夹，缺少的上级目录会一并创建。", map[string]any{"root": field("目录 id"), "path": field("相对目录路径")}, []string{"root", "path"}},
		{"write_file", "新建文件或整体重写。覆盖已有文件时必须使用最近读取的 SHA-256；新建使用 new。缺少的上级目录会自动创建。只改一处时请用 edit_file。", map[string]any{"root": field("目录 id"), "path": field("相对文件路径"), "text": field("完整文件内容"), "expected_sha256": field("现有内容哈希或 new")}, []string{"root", "path", "text", "expected_sha256"}},
		{"move_file", "在同一授权目录内移动或重命名文件、文件夹。目标已存在时拒绝，不会覆盖；缺少的上级目录会自动创建。", map[string]any{"root": field("目录 id"), "path": field("要移动的相对路径"), "destination": field("目标相对路径")}, []string{"root", "path", "destination"}},
		{"search_text", "在本地目录搜索文本，最多 100 条结果，返回的 path 相对授权目录。默认原样匹配；regex 为 true 时按 Go 正则匹配，可用 (?i) 忽略大小写。include 按文件名过滤。" + skipNote, map[string]any{"root": field("目录 id"), "path": field("相对目录，默认 ."), "query": field("搜索文本或正则"), "regex": map[string]any{"type": "boolean", "description": "是否按正则匹配，默认 false"}, "include": field("可选：文件名通配符，例如 *.go；规则同 find_files")}, []string{"root", "query"}},
		{"find_files", "按文件名通配符查找文件，例如 *.go、*_test.py、config*.json；只匹配文件名，不支持 {a,b} 和含 / 的模式（开头的 **/ 可以省略），要限定目录请用 path。最多返回 200 个，路径相对授权目录。" + skipNote, map[string]any{"root": field("目录 id"), "path": field("相对目录，默认 ."), "pattern": field("文件名通配符")}, []string{"root", "pattern"}},
	}
	if allowExec {
		definitions = append(definitions, struct {
			name, description string
			props             map[string]any
			required          []string
		}{"run_command", "在本地项目目录启动程序。program 为可执行文件，args 为参数数组，不自动解析 Shell。超时最多 120 秒；返回输出和退出码。", map[string]any{"root": field("目录 id"), "path": field("相对工作目录，默认 ."), "program": field("例如 git、node、python3，或 powershell.exe"), "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 120}}, []string{"root", "program"}})
	}
	out := []any{}
	for _, d := range definitions {
		out = append(out, map[string]any{"name": d.name, "description": d.description, "inputSchema": map[string]any{"type": "object", "properties": d.props, "required": d.required, "additionalProperties": false}})
	}
	return out
}

func isPermissionError(err error) bool { return os.IsPermission(err) }
