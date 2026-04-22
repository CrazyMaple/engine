package codegen

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ParseProtoFile 解析 .proto 文件，提取消息定义为 []MessageDef
// 仅支持 proto3 语法，处理 message 和 enum 块
// 消息 ID 从 2001 起自增（避免与 Go 源文件解析的 1001+ 冲突）
func ParseProtoFile(filename string) ([]MessageDef, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filename, err)
	}
	defer f.Close()

	var (
		msgs       []MessageDef
		nextID     = 2001
		state      = stateOutside
		currentMsg *MessageDef
		comment    string // 累积的块前注释
		depth      int    // 嵌套大括号深度
	)

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行
		if line == "" {
			comment = ""
			continue
		}

		// 提取行注释
		if strings.HasPrefix(line, "//") {
			text := strings.TrimPrefix(line, "//")
			text = strings.TrimSpace(text)
			comment = text
			continue
		}

		// 去掉行内注释
		inlineComment := ""
		if idx := strings.Index(line, "//"); idx >= 0 {
			inlineComment = strings.TrimSpace(line[idx+2:])
			line = strings.TrimSpace(line[:idx])
		}

		switch state {
		case stateOutside:
			// 跳过 syntax、package、option、import、service 等
			if strings.HasPrefix(line, "syntax") ||
				strings.HasPrefix(line, "package") ||
				strings.HasPrefix(line, "option") ||
				strings.HasPrefix(line, "import") ||
				strings.HasPrefix(line, "service") {
				comment = ""
				continue
			}

			// 检测 message 块
			if strings.HasPrefix(line, "message ") {
				name := extractBlockName(line)
				if name == "" {
					return nil, fmt.Errorf("%s:%d: invalid message declaration", filename, lineNum)
				}
				currentMsg = &MessageDef{
					Name:    name,
					ID:      nextID,
					Comment: comment,
				}
				nextID++
				comment = ""
				depth = 1
				state = stateInsideMessage
				continue
			}

			// 检测 enum 块（跳过内容）
			if strings.HasPrefix(line, "enum ") {
				depth = 1
				state = stateInsideEnum
				comment = ""
				continue
			}

			comment = ""

		case stateInsideMessage:
			// 跟踪大括号深度（处理嵌套 message/oneof）
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if depth <= 0 {
				// 消息块结束
				msgs = append(msgs, *currentMsg)
				currentMsg = nil
				state = stateOutside
				comment = ""
				continue
			}

			// 跳过嵌套 message / oneof / enum / reserved / option
			if strings.HasPrefix(line, "message ") ||
				strings.HasPrefix(line, "oneof ") ||
				strings.HasPrefix(line, "enum ") ||
				strings.HasPrefix(line, "reserved ") ||
				strings.HasPrefix(line, "option ") ||
				line == "}" {
				comment = ""
				continue
			}

			// 解析字段行：[repeated] type name = number;
			fd, err := parseProtoField(line)
			if err != nil {
				// 不可解析的行静默跳过（map 类型等）
				comment = ""
				continue
			}
			if inlineComment != "" {
				fd.Comment = inlineComment
			} else if comment != "" {
				fd.Comment = comment
			}
			currentMsg.Fields = append(currentMsg.Fields, fd)
			comment = ""

		case stateInsideEnum:
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if depth <= 0 {
				state = stateOutside
				comment = ""
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", filename, err)
	}

	if state == stateInsideMessage {
		return nil, fmt.Errorf("%s: unexpected end of file inside message block", filename)
	}

	return msgs, nil
}

// 解析器状态
const (
	stateOutside       = 0
	stateInsideMessage = 1
	stateInsideEnum    = 2
)

// extractBlockName 从 "message Foo {" 或 "message Foo{" 中提取名称
func extractBlockName(line string) string {
	// 去掉 { 和之后的内容
	line = strings.TrimSuffix(line, "{")
	line = strings.TrimSpace(line)
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// parseProtoField 解析一行 proto 字段定义
// 格式: [repeated] type name = number;
func parseProtoField(line string) (FieldDef, error) {
	line = strings.TrimSuffix(line, ";")
	line = strings.TrimSpace(line)

	// 处理 map 类型：map<key, value> name = number
	if strings.HasPrefix(line, "map<") {
		return parseMapField(line)
	}

	parts := strings.Fields(line)
	if len(parts) < 4 {
		return FieldDef{}, fmt.Errorf("invalid field: %s", line)
	}

	repeated := false
	offset := 0
	if parts[0] == "repeated" || parts[0] == "optional" {
		if parts[0] == "repeated" {
			repeated = true
		}
		offset = 1
	}

	if len(parts) < offset+4 {
		return FieldDef{}, fmt.Errorf("invalid field: %s", line)
	}

	protoType := parts[offset]
	name := parts[offset+1]
	// parts[offset+2] should be "="
	// parts[offset+3] should be the field number

	goType := protoTypeToGoType(protoType, repeated)
	jsonName := protoFieldToJSONName(name)

	return FieldDef{
		Name:     protoNameToGoName(name),
		Type:     goType,
		JSONName: jsonName,
	}, nil
}

// parseMapField 解析 map<K,V> 字段
func parseMapField(line string) (FieldDef, error) {
	// map<string, int32> field_name = N
	closeBracket := strings.Index(line, ">")
	if closeBracket < 0 {
		return FieldDef{}, fmt.Errorf("invalid map field: %s", line)
	}
	mapSpec := line[4:closeBracket] // "string, int32"
	rest := strings.TrimSpace(line[closeBracket+1:])
	parts := strings.Fields(rest)
	if len(parts) < 3 {
		return FieldDef{}, fmt.Errorf("invalid map field: %s", line)
	}

	kvParts := strings.SplitN(mapSpec, ",", 2)
	if len(kvParts) != 2 {
		return FieldDef{}, fmt.Errorf("invalid map field: %s", line)
	}
	keyType := protoTypeToGoType(strings.TrimSpace(kvParts[0]), false)
	valType := protoTypeToGoType(strings.TrimSpace(kvParts[1]), false)
	goType := "map[" + keyType + "]" + valType

	name := parts[0]
	jsonName := protoFieldToJSONName(name)

	return FieldDef{
		Name:     protoNameToGoName(name),
		Type:     goType,
		JSONName: jsonName,
	}, nil
}

// protoTypeToGoType 将 proto3 类型名转换为 Go 类型名
func protoTypeToGoType(protoType string, repeated bool) string {
	var goType string
	switch protoType {
	case "double":
		goType = "float64"
	case "float":
		goType = "float32"
	case "int32", "sint32", "sfixed32":
		goType = "int32"
	case "int64", "sint64", "sfixed64":
		goType = "int64"
	case "uint32", "fixed32":
		goType = "uint32"
	case "uint64", "fixed64":
		goType = "uint64"
	case "bool":
		goType = "bool"
	case "string":
		goType = "string"
	case "bytes":
		goType = "[]byte"
	default:
		// 自定义消息类型引用
		goType = protoType
	}
	if repeated {
		goType = "[]" + goType
	}
	return goType
}

// protoNameToGoName 将 snake_case 转为 PascalCase
func protoNameToGoName(name string) string {
	parts := strings.Split(name, "_")
	var result strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if isCommonAcronym(strings.ToUpper(p)) {
			result.WriteString(strings.ToUpper(p))
		} else {
			result.WriteString(strings.ToUpper(p[:1]))
			result.WriteString(p[1:])
		}
	}
	return result.String()
}

// protoFieldToJSONName 将 proto 字段名作为 JSON 名（proto3 默认 snake_case）
func protoFieldToJSONName(name string) string {
	return name
}

// isCommonAcronym 检查是否为常见缩写词（保持全大写）
func isCommonAcronym(s string) bool {
	acronyms := map[string]bool{
		"ID": true, "IP": true, "URL": true, "HTTP": true,
		"API": true, "PID": true, "CPU": true, "GC": true,
	}
	return acronyms[s]
}

// _ 确保 strconv 被使用（用于后续扩展如消息 ID 显式赋值）
var _ = strconv.Itoa
