package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// --- OpenAPI 风格协议文档生成 ---

// OpenAPIDoc 简化版 OpenAPI 风格文档结构（面向游戏协议而非 HTTP REST）
type OpenAPIDoc struct {
	Info     DocInfo               `json:"info"`
	Messages map[string]DocMessage `json:"messages"`
}

// DocInfo 文档元信息
type DocInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// DocMessage 消息文档
type DocMessage struct {
	Name        string     `json:"name"`
	ID          int        `json:"id"`
	Description string     `json:"description,omitempty"`
	Fields      []DocField `json:"fields,omitempty"`
	Direction   string     `json:"direction,omitempty"` // C2S / S2C / Both
}

// DocField 字段文档
type DocField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	JSONName    string `json:"json_name"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

// GenerateOpenAPIDoc 生成 OpenAPI 风格的 JSON 协议文档
func GenerateOpenAPIDoc(msgs []MessageDef, title, version string) ([]byte, error) {
	doc := OpenAPIDoc{
		Info: DocInfo{
			Title:   title,
			Version: version,
		},
		Messages: make(map[string]DocMessage, len(msgs)),
	}

	for _, msg := range msgs {
		dm := DocMessage{
			Name:        msg.Name,
			ID:          msg.ID,
			Description: msg.Comment,
			Direction:   inferDirection(msg.Name),
		}
		for _, f := range msg.Fields {
			df := DocField{
				Name:        f.Name,
				Type:        f.Type,
				JSONName:    f.JSONName,
				Required:    !strings.Contains(f.Tag, "omitempty"),
				Description: f.Comment,
			}
			dm.Fields = append(dm.Fields, df)
		}
		doc.Messages[msg.Name] = dm
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal openapi doc: %w", err)
	}
	return data, nil
}

// inferDirection 从消息名推断消息方向
func inferDirection(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "request") || strings.HasPrefix(lower, "req"):
		return "C2S"
	case strings.HasSuffix(lower, "response") || strings.HasSuffix(lower, "reply"):
		return "S2C"
	case strings.HasSuffix(lower, "notify") || strings.HasSuffix(lower, "event") || strings.HasSuffix(lower, "push"):
		return "S2C"
	default:
		return "Both"
	}
}

// --- HTML 可浏览协议文档 ---

const htmlDocTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} - Protocol Reference</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,sans-serif;background:#f5f5f5;color:#333;padding:20px;max-width:1200px;margin:0 auto}
h1{color:#2c3e50;margin-bottom:4px;font-size:28px}
.subtitle{color:#7f8c8d;margin-bottom:20px;font-size:14px}
.search{width:100%;padding:10px;border:1px solid #ddd;border-radius:4px;margin-bottom:20px;font-size:16px}
.msg-card{background:white;border:1px solid #e0e0e0;border-radius:8px;padding:16px;margin-bottom:12px}
.msg-card h2{color:#2980b9;font-size:18px;margin-bottom:8px}
.msg-card .meta{font-size:12px;color:#999;margin-bottom:8px}
.msg-card .desc{color:#555;margin-bottom:12px;font-size:14px}
.dir-c2s{color:#e74c3c;font-weight:bold}
.dir-s2c{color:#27ae60;font-weight:bold}
.dir-both{color:#f39c12;font-weight:bold}
table{width:100%;border-collapse:collapse;font-size:14px}
th{background:#ecf0f1;text-align:left;padding:8px;font-weight:600}
td{padding:8px;border-bottom:1px solid #eee}
.type{color:#8e44ad;font-family:monospace}
.required{color:#e74c3c;font-size:12px}
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<p class="subtitle">Version: {{.Version}} | Messages: {{.Count}}</p>
<input class="search" type="text" placeholder="Search messages..." onkeyup="filterMsgs(this.value)">
<div id="messages">
{{range .Messages}}
<div class="msg-card" data-name="{{lower .Name}}">
  <h2>{{.Name}} <span class="meta">ID: {{.ID}}</span>
    {{if eq .Direction "C2S"}}<span class="dir-c2s">[C2S]</span>{{end}}
    {{if eq .Direction "S2C"}}<span class="dir-s2c">[S2C]</span>{{end}}
    {{if eq .Direction "Both"}}<span class="dir-both">[Both]</span>{{end}}
  </h2>
  {{if .Description}}<p class="desc">{{.Description}}</p>{{end}}
  {{if .Fields}}
  <table>
    <tr><th>Field</th><th>Type</th><th>JSON</th><th>Description</th></tr>
    {{range .Fields}}
    <tr>
      <td>{{.Name}}{{if .Required}} <span class="required">*</span>{{end}}</td>
      <td class="type">{{.Type}}</td>
      <td>{{.JSONName}}</td>
      <td>{{.Description}}</td>
    </tr>
    {{end}}
  </table>
  {{else}}<p style="color:#999">No fields</p>{{end}}
</div>
{{end}}
</div>
<script>
function filterMsgs(q){
  q=q.toLowerCase();
  document.querySelectorAll('.msg-card').forEach(function(c){
    c.style.display=c.dataset.name.includes(q)?'':'none';
  });
}
</script>
</body>
</html>`

// GenerateHTMLDoc 生成 HTML 可浏览协议文档
func GenerateHTMLDoc(msgs []MessageDef, title, version string) ([]byte, error) {
	type viewMsg struct {
		Name, Description, Direction string
		ID                          int
		Fields                      []DocField
	}

	viewMsgs := make([]viewMsg, 0, len(msgs))
	for _, msg := range msgs {
		vm := viewMsg{
			Name:        msg.Name,
			ID:          msg.ID,
			Description: msg.Comment,
			Direction:   inferDirection(msg.Name),
		}
		for _, f := range msg.Fields {
			vm.Fields = append(vm.Fields, DocField{
				Name:        f.Name,
				Type:        f.Type,
				JSONName:    f.JSONName,
				Required:    !strings.Contains(f.Tag, "omitempty"),
				Description: f.Comment,
			})
		}
		viewMsgs = append(viewMsgs, vm)
	}

	data := struct {
		Title    string
		Version  string
		Count    int
		Messages []viewMsg
	}{
		Title:    title,
		Version:  version,
		Count:    len(viewMsgs),
		Messages: viewMsgs,
	}

	tmpl, err := template.New("html").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).Parse(htmlDocTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse html template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute html template: %w", err)
	}
	return buf.Bytes(), nil
}

// --- 协议变更日志自动生成 ---

// GenerateChangelog 对比两个版本清单，生成 Markdown 变更日志
func GenerateChangelog(old, new *VersionManifest) ([]byte, error) {
	diff := CompareManifests(old, new)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# Protocol Changelog: v%d → v%d\n\n", old.ProtocolVersion, new.ProtocolVersion)

	if len(diff.AddedMessages) == 0 && len(diff.RemovedMessages) == 0 && len(diff.ChangedMessages) == 0 {
		fmt.Fprintln(&buf, "No changes.")
		return buf.Bytes(), nil
	}

	// 新增消息
	if len(diff.AddedMessages) > 0 {
		fmt.Fprintf(&buf, "## Added Messages (%d)\n\n", len(diff.AddedMessages))
		sort.Strings(diff.AddedMessages)
		for _, name := range diff.AddedMessages {
			fmt.Fprintf(&buf, "- `%s`\n", name)
		}
		fmt.Fprintln(&buf)
	}

	// 删除消息
	if len(diff.RemovedMessages) > 0 {
		fmt.Fprintf(&buf, "## Removed Messages (%d)\n\n", len(diff.RemovedMessages))
		sort.Strings(diff.RemovedMessages)
		for _, name := range diff.RemovedMessages {
			fmt.Fprintf(&buf, "- ~~`%s`~~\n", name)
		}
		fmt.Fprintln(&buf)
	}

	// 变更消息
	if len(diff.ChangedMessages) > 0 {
		fmt.Fprintf(&buf, "## Changed Messages (%d)\n\n", len(diff.ChangedMessages))
		sort.Slice(diff.ChangedMessages, func(i, j int) bool {
			return diff.ChangedMessages[i].Name < diff.ChangedMessages[j].Name
		})
		for _, ch := range diff.ChangedMessages {
			fmt.Fprintf(&buf, "### `%s`\n\n", ch.Name)
			for _, f := range ch.AddedFields {
				fmt.Fprintf(&buf, "- **Added** field `%s` (%s)\n", f.Name, f.Type)
			}
			for _, f := range ch.RemovedFields {
				fmt.Fprintf(&buf, "- **Removed** field `%s` (%s)\n", f.Name, f.Type)
			}
			for _, f := range ch.ChangedFields {
				fmt.Fprintf(&buf, "- **Changed** field `%s`: `%s` → `%s`\n", f.Name, f.OldType, f.NewType)
			}
			fmt.Fprintln(&buf)
		}
	}

	return buf.Bytes(), nil
}

// --- Mock Server ---

// MockResponse 根据消息定义生成 Mock 响应数据
func MockResponse(msg MessageDef) map[string]interface{} {
	result := make(map[string]interface{}, len(msg.Fields))
	for _, f := range msg.Fields {
		result[f.JSONName] = mockValue(f.Type)
	}
	return result
}

// mockValue 根据 Go 类型生成 Mock 值
func mockValue(goType string) interface{} {
	switch goType {
	case "string":
		return "mock_string"
	case "int", "int32", "int64":
		return 0
	case "uint", "uint32", "uint64":
		return 0
	case "float32", "float64":
		return 0.0
	case "bool":
		return false
	default:
		if strings.HasPrefix(goType, "[]") {
			elemType := goType[2:]
			return []interface{}{mockValue(elemType)}
		}
		if strings.HasPrefix(goType, "map[") {
			return map[string]interface{}{}
		}
		return nil
	}
}

// GenerateMockServer 生成 Mock Server 的 Go 代码
func GenerateMockServer(msgs []MessageDef, pkg string) ([]byte, error) {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "package %s\n\n", pkg)
	fmt.Fprintln(&buf, `import (`)
	fmt.Fprintln(&buf, `	"encoding/json"`)
	fmt.Fprintln(&buf, `	"net/http"`)
	fmt.Fprintln(&buf, `)`)
	fmt.Fprintln(&buf)

	// 按消息名排序
	sorted := make([]MessageDef, len(msgs))
	copy(sorted, msgs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	fmt.Fprintln(&buf, `// RegisterMockHandlers 注册 Mock 响应处理器`)
	fmt.Fprintln(&buf, `func RegisterMockHandlers(mux *http.ServeMux) {`)

	for _, msg := range sorted {
		// 只为 Response/Reply 消息生成 Mock
		if !isResponseMsg(msg.Name) {
			continue
		}
		route := "/mock/" + strings.ToLower(msg.Name)
		mockData := MockResponse(msg)
		jsonBytes, _ := json.MarshalIndent(mockData, "\t\t", "  ")

		fmt.Fprintf(&buf, "\tmux.HandleFunc(%q, func(w http.ResponseWriter, r *http.Request) {\n", route)
		fmt.Fprintln(&buf, "\t\tw.Header().Set(\"Content-Type\", \"application/json\")")
		fmt.Fprintf(&buf, "\t\tw.Write([]byte(`%s`))\n", string(jsonBytes))
		fmt.Fprintln(&buf, "\t})")
		fmt.Fprintln(&buf)
	}

	fmt.Fprintln(&buf, "}")
	return buf.Bytes(), nil
}

// isResponseMsg 判断是否为响应类消息
func isResponseMsg(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "response") ||
		strings.HasSuffix(lower, "reply") ||
		strings.HasSuffix(lower, "result") ||
		strings.HasSuffix(lower, "notify") ||
		strings.HasSuffix(lower, "push") ||
		strings.HasSuffix(lower, "event")
}
