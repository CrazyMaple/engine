package hotreload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"sync"
)

// --- Go Plugin 加载器 ---
// 通过 Go plugin 包（.so 动态库）实现热更新。
// 适用于重逻辑变更（新系统、新功能），需重新编译。

// GoPluginFactory 从 .so 文件创建 Plugin 的工厂函数类型
// .so 文件必须导出 NewPlugin 函数：func NewPlugin() hotreload.Plugin
type GoPluginFactory func() Plugin

// GoPluginLoader Go Plugin 动态加载器
type GoPluginLoader struct {
	mu          sync.RWMutex
	pluginDir   string
	loaded      map[string]*plugin.Plugin
	factoryName string // .so 中的工厂函数名，默认 "NewPlugin"
}

// NewGoPluginLoader 创建 Go Plugin 加载器
func NewGoPluginLoader(pluginDir string) *GoPluginLoader {
	return &GoPluginLoader{
		pluginDir:   pluginDir,
		loaded:      make(map[string]*plugin.Plugin),
		factoryName: "NewPlugin",
	}
}

// LoadFromFile 从 .so 文件加载插件
func (l *GoPluginLoader) LoadFromFile(soPath string) (Plugin, error) {
	absPath, err := filepath.Abs(soPath)
	if err != nil {
		return nil, fmt.Errorf("hotreload: resolve path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("hotreload: plugin file not found: %s", absPath)
	}

	p, err := plugin.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("hotreload: open plugin: %w", err)
	}

	sym, err := p.Lookup(l.factoryName)
	if err != nil {
		return nil, fmt.Errorf("hotreload: lookup %s: %w", l.factoryName, err)
	}

	factory, ok := sym.(func() Plugin)
	if !ok {
		// 尝试无返回值的函数签名
		return nil, fmt.Errorf("hotreload: %s has wrong signature, expected func() Plugin", l.factoryName)
	}

	pluginInstance := factory()
	if pluginInstance == nil {
		return nil, errors.New("hotreload: factory returned nil plugin")
	}

	l.mu.Lock()
	l.loaded[pluginInstance.Name()] = p
	l.mu.Unlock()

	return pluginInstance, nil
}

// ScanDir 扫描插件目录中的所有 .so 文件
func (l *GoPluginLoader) ScanDir() ([]string, error) {
	if l.pluginDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(l.pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".so" {
			files = append(files, filepath.Join(l.pluginDir, entry.Name()))
		}
	}
	return files, nil
}
