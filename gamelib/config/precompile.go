package config

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"time"
)

// PrecompiledCache 预编译二进制缓存
type PrecompiledCache struct {
	Version uint32    // 缓存格式版本
	ModTime int64     // 源文件修改时间（Unix 秒）
	Records []byte    // gob 编码的记录数据
}

const cacheVersion uint32 = 1

// Precompile 将 RecordFile 序列化为二进制缓存文件
func Precompile(rf *RecordFile, sourceFile, cacheFile string) error {
	info, err := os.Stat(sourceFile)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	// gob 编码记录
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(rf.records); err != nil {
		return fmt.Errorf("encode records: %w", err)
	}

	cache := PrecompiledCache{
		Version: cacheVersion,
		ModTime: info.ModTime().Unix(),
		Records: buf.Bytes(),
	}

	var fileBuf bytes.Buffer
	if err := gob.NewEncoder(&fileBuf).Encode(cache); err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}

	return os.WriteFile(cacheFile, fileBuf.Bytes(), 0644)
}

// LoadPrecompiled 尝试加载预编译缓存
// 如果缓存文件不存在或源文件已修改，返回 (nil, nil)
func LoadPrecompiled(sourceFile, cacheFile string) ([]interface{}, error) {
	// 检查缓存文件是否存在
	cacheData, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, nil // 缓存不存在，不是错误
	}

	// 检查源文件修改时间
	sourceInfo, err := os.Stat(sourceFile)
	if err != nil {
		return nil, nil
	}

	// 解码缓存
	var cache PrecompiledCache
	if err := gob.NewDecoder(bytes.NewReader(cacheData)).Decode(&cache); err != nil {
		return nil, nil // 缓存损坏，忽略
	}

	// 版本检查
	if cache.Version != cacheVersion {
		return nil, nil
	}

	// 时间戳检查
	if cache.ModTime != sourceInfo.ModTime().Unix() {
		return nil, nil // 源文件已修改，缓存过期
	}

	// 解码记录
	var records []interface{}
	if err := gob.NewDecoder(bytes.NewReader(cache.Records)).Decode(&records); err != nil {
		return nil, nil // 解码失败，忽略缓存
	}

	return records, nil
}

// CacheStaleAfter 检查缓存文件是否在指定时间之前创建（用于主动清理）
func CacheStaleAfter(cacheFile string, age time.Duration) bool {
	info, err := os.Stat(cacheFile)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > age
}
