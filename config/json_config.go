package config

import (
	"encoding/json"
	"os"
)

// LoadJSON 从 JSON 文件加载配置到目标结构体
func LoadJSON(filename string, target interface{}) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// SaveJSON 将配置结构体保存为 JSON 文件
func SaveJSON(filename string, source interface{}) error {
	data, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}
