package hotreload

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// --- 热更新框架核心接口与管理器 ---

var (
	ErrPluginNotFound    = errors.New("hotreload: plugin not found")
	ErrPluginLoaded      = errors.New("hotreload: plugin already loaded")
	ErrVersionMismatch   = errors.New("hotreload: plugin version mismatch")
	ErrRollbackFailed    = errors.New("hotreload: rollback failed")
)

// Plugin 可热更新的逻辑单元接口
type Plugin interface {
	// Name 插件唯一名称
	Name() string
	// Version 插件版本号
	Version() string
	// Load 加载插件（首次加载时调用）
	Load() error
	// Reload 重新加载插件（热更新时调用，接收旧版本供状态迁移）
	Reload(old Plugin) error
	// Unload 卸载插件（停止时调用，用于清理资源）
	Unload() error
	// Health 健康检查（加载后定期检查插件是否正常）
	Health() error
}

// PluginInfo 插件运行时信息
type PluginInfo struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Status    string    `json:"status"` // loaded / unloaded / error
	LoadedAt  time.Time `json:"loaded_at"`
	Error     string    `json:"error,omitempty"`
}

// ReloadEvent 热更新事件
type ReloadEvent struct {
	PluginName string    `json:"plugin_name"`
	OldVersion string    `json:"old_version"`
	NewVersion string    `json:"new_version"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	Time       time.Time `json:"time"`
}

// Manager 热更新管理器
type Manager struct {
	mu       sync.RWMutex
	plugins  map[string]*pluginEntry
	history  []ReloadEvent
	maxHist  int
	listener func(ReloadEvent) // 可选的事件监听器
}

type pluginEntry struct {
	plugin   Plugin
	info     PluginInfo
	previous Plugin // 上一版本，用于回滚
}

// NewManager 创建热更新管理器
func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]*pluginEntry),
		maxHist: 100,
	}
}

// SetEventListener 设置热更新事件监听器
func (m *Manager) SetEventListener(fn func(ReloadEvent)) {
	m.mu.Lock()
	m.listener = fn
	m.mu.Unlock()
}

// Load 加载插件
func (m *Manager) Load(p Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := p.Name()
	if _, exists := m.plugins[name]; exists {
		return ErrPluginLoaded
	}

	if err := p.Load(); err != nil {
		return fmt.Errorf("hotreload: load %s: %w", name, err)
	}

	m.plugins[name] = &pluginEntry{
		plugin: p,
		info: PluginInfo{
			Name:     name,
			Version:  p.Version(),
			Status:   "loaded",
			LoadedAt: time.Now(),
		},
	}
	return nil
}

// Reload 热更新插件（用新版本替换旧版本）
func (m *Manager) Reload(newPlugin Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := newPlugin.Name()
	entry, exists := m.plugins[name]
	if !exists {
		return ErrPluginNotFound
	}

	oldPlugin := entry.plugin
	oldVersion := entry.info.Version

	// 执行热更新
	if err := newPlugin.Reload(oldPlugin); err != nil {
		event := ReloadEvent{
			PluginName: name,
			OldVersion: oldVersion,
			NewVersion: newPlugin.Version(),
			Success:    false,
			Error:      err.Error(),
			Time:       time.Now(),
		}
		m.appendEvent(event)
		return fmt.Errorf("hotreload: reload %s: %w", name, err)
	}

	// 更新插件条目
	entry.previous = oldPlugin
	entry.plugin = newPlugin
	entry.info = PluginInfo{
		Name:     name,
		Version:  newPlugin.Version(),
		Status:   "loaded",
		LoadedAt: time.Now(),
	}

	event := ReloadEvent{
		PluginName: name,
		OldVersion: oldVersion,
		NewVersion: newPlugin.Version(),
		Success:    true,
		Time:       time.Now(),
	}
	m.appendEvent(event)

	return nil
}

// Rollback 回滚到上一版本
func (m *Manager) Rollback(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.plugins[name]
	if !exists {
		return ErrPluginNotFound
	}

	if entry.previous == nil {
		return ErrRollbackFailed
	}

	current := entry.plugin
	prev := entry.previous
	currentVersion := entry.info.Version

	// 重新加载旧版本
	if err := prev.Reload(current); err != nil {
		event := ReloadEvent{
			PluginName: name,
			OldVersion: currentVersion,
			NewVersion: prev.Version(),
			Success:    false,
			Error:      "rollback: " + err.Error(),
			Time:       time.Now(),
		}
		m.appendEvent(event)
		return fmt.Errorf("hotreload: rollback %s: %w", name, err)
	}

	entry.plugin = prev
	entry.previous = nil
	entry.info = PluginInfo{
		Name:     name,
		Version:  prev.Version(),
		Status:   "loaded",
		LoadedAt: time.Now(),
	}

	event := ReloadEvent{
		PluginName: name,
		OldVersion: currentVersion,
		NewVersion: prev.Version(),
		Success:    true,
		Time:       time.Now(),
	}
	m.appendEvent(event)

	return nil
}

// Unload 卸载插件
func (m *Manager) Unload(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.plugins[name]
	if !exists {
		return ErrPluginNotFound
	}

	if err := entry.plugin.Unload(); err != nil {
		entry.info.Status = "error"
		entry.info.Error = err.Error()
		return fmt.Errorf("hotreload: unload %s: %w", name, err)
	}

	delete(m.plugins, name)
	return nil
}

// Get 获取已加载的插件
func (m *Manager) Get(name string) (Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.plugins[name]
	if !ok {
		return nil, false
	}
	return entry.plugin, true
}

// List 列出所有已加载插件信息
func (m *Manager) List() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]PluginInfo, 0, len(m.plugins))
	for _, entry := range m.plugins {
		infos = append(infos, entry.info)
	}
	return infos
}

// ReloadHistory 返回最近的热更新事件
func (m *Manager) ReloadHistory(n int) []ReloadEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if n <= 0 || len(m.history) == 0 {
		return nil
	}
	start := len(m.history) - n
	if start < 0 {
		start = 0
	}
	result := make([]ReloadEvent, len(m.history)-start)
	copy(result, m.history[start:])
	return result
}

func (m *Manager) appendEvent(event ReloadEvent) {
	if len(m.history) >= m.maxHist {
		m.history = m.history[1:]
	}
	m.history = append(m.history, event)

	if m.listener != nil {
		go m.listener(event)
	}
}
