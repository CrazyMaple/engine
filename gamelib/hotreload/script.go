package hotreload

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// --- 脚本热更新支持 ---
// 提供轻量级脚本热更新框架。
// 当前实现为文件监听 + 回调模式，可桥接 Lua/JS 等脚本引擎。

// ScriptEngine 脚本引擎接口
type ScriptEngine interface {
	// Name 引擎名称
	Name() string
	// Eval 执行脚本内容
	Eval(script string) (interface{}, error)
	// LoadFile 加载并执行脚本文件
	LoadFile(path string) error
	// Close 关闭引擎
	Close() error
}

// ScriptPlugin 基于脚本文件的插件
type ScriptPlugin struct {
	name     string
	version  string
	filePath string
	engine   ScriptEngine
	content  string
	mu       sync.RWMutex
}

// NewScriptPlugin 创建脚本插件
func NewScriptPlugin(name, version, filePath string, engine ScriptEngine) *ScriptPlugin {
	return &ScriptPlugin{
		name:     name,
		version:  version,
		filePath: filePath,
		engine:   engine,
	}
}

func (p *ScriptPlugin) Name() string    { return p.name }
func (p *ScriptPlugin) Version() string { return p.version }

func (p *ScriptPlugin) Load() error {
	content, err := os.ReadFile(p.filePath)
	if err != nil {
		return fmt.Errorf("read script: %w", err)
	}

	p.mu.Lock()
	p.content = string(content)
	p.mu.Unlock()

	return p.engine.LoadFile(p.filePath)
}

func (p *ScriptPlugin) Reload(old Plugin) error {
	return p.Load()
}

func (p *ScriptPlugin) Unload() error {
	return p.engine.Close()
}

func (p *ScriptPlugin) Health() error {
	_, err := p.engine.Eval("true")
	return err
}

// ScriptWatcher 脚本文件监听器，检测文件变更自动触发热更新
type ScriptWatcher struct {
	manager  *Manager
	files    map[string]time.Time // path -> last mod time
	interval time.Duration
	stopChan chan struct{}
	mu       sync.Mutex
	factory  func(path string) Plugin // 从文件路径创建新版本插件
}

// NewScriptWatcher 创建脚本文件监听器
func NewScriptWatcher(manager *Manager, interval time.Duration) *ScriptWatcher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &ScriptWatcher{
		manager:  manager,
		files:    make(map[string]time.Time),
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

// SetFactory 设置插件工厂函数
func (w *ScriptWatcher) SetFactory(fn func(path string) Plugin) {
	w.mu.Lock()
	w.factory = fn
	w.mu.Unlock()
}

// Watch 添加监听文件
func (w *ScriptWatcher) Watch(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	info, err := os.Stat(path)
	if err == nil {
		w.files[path] = info.ModTime()
	} else {
		w.files[path] = time.Time{}
	}
}

// Start 开始监听
func (w *ScriptWatcher) Start() {
	go w.watchLoop()
}

// Stop 停止监听
func (w *ScriptWatcher) Stop() {
	close(w.stopChan)
}

func (w *ScriptWatcher) watchLoop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.checkChanges()
		case <-w.stopChan:
			return
		}
	}
}

func (w *ScriptWatcher) checkChanges() {
	w.mu.Lock()
	factory := w.factory
	files := make(map[string]time.Time, len(w.files))
	for k, v := range w.files {
		files[k] = v
	}
	w.mu.Unlock()

	if factory == nil {
		return
	}

	for path, lastMod := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		if info.ModTime().After(lastMod) {
			// 文件已变更，触发热更新
			newPlugin := factory(path)
			if newPlugin != nil {
				_ = w.manager.Reload(newPlugin)
			}

			w.mu.Lock()
			w.files[path] = info.ModTime()
			w.mu.Unlock()
		}
	}
}
