package codegen

import "strings"

// goTypeToCSharp 将 Go 类型转换为 C# 类型
func goTypeToCSharp(goType string) string {
	switch goType {
	case "int", "int32":
		return "int"
	case "int8":
		return "sbyte"
	case "int16":
		return "short"
	case "int64":
		return "long"
	case "uint", "uint32":
		return "uint"
	case "uint8", "byte":
		return "byte"
	case "uint16":
		return "ushort"
	case "uint64":
		return "ulong"
	case "float32":
		return "float"
	case "float64":
		return "double"
	case "string":
		return "string"
	case "bool":
		return "bool"
	default:
		if strings.HasPrefix(goType, "[]") {
			return "List<" + goTypeToCSharp(goType[2:]) + ">"
		}
		if strings.HasPrefix(goType, "map[") {
			// map[K]V → Dictionary<K,V>
			closing := strings.Index(goType, "]")
			if closing > 4 {
				key := goType[4:closing]
				val := goType[closing+1:]
				return "Dictionary<" + goTypeToCSharp(key) + ", " + goTypeToCSharp(val) + ">"
			}
		}
		return "object"
	}
}

// goTypeToJSONDesc 将 Go 类型转换为 JSON 类型描述（用于文档）
func goTypeToJSONDesc(goType string) string {
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "integer"
	case "float32", "float64":
		return "number"
	case "string":
		return "string"
	case "bool":
		return "boolean"
	default:
		if strings.HasPrefix(goType, "[]") {
			return "array<" + goTypeToJSONDesc(goType[2:]) + ">"
		}
		if strings.HasPrefix(goType, "map[") {
			return "object"
		}
		return "object"
	}
}
