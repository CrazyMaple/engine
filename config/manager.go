package config

import (
	"os"
	"sync"
	"time"

	"engine/log"
)

// EntryType 配置条目类型
type EntryType int

const (
	EntryTypeRecordFile EntryType = iota
	EntryTypeJSON
)

// ConfigEntry 配置条目
type ConfigEntry struct {
	Filename   string
	Type       EntryType
	Prototype  interface{}  // RecordFile 的结构体原型
	RecordFile *RecordFile  // 加载后的 RecordFile
	JSONTarget interface{}  // JSON 配置目标
	ModTime    time.Time    // 最后修改时间
	OnReload   func()       // 重载回调
}

// Manager 配置管理器，支持多配置文件加载和热重载
type Manager struct {
	mu       sync.RWMutex
	entries  map[string]*ConfigEntry
	stopCh   chan struct{}
	watching bool
}

// NewManager 创建配置管理器
func NewManager() *Manager {
	return &Manager{
		entries: make(map[string]*ConfigEntry),
	}
}

// RegisterRecordFile 注册 RecordFile 配置
// prototype 是结构体实例（非指针），如 ItemConfig{}
func (m *Manager) RegisterRecordFile(filename string, prototype interface{}, onReload func()) error {
	rf, err := NewRecordFile(prototype)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.entries[filename] = &ConfigEntry{
		Filename:  filename,
		Type:      EntryTypeRecordFile,
		Prototype: prototype,
		RecordFile: rf,
		OnReload:  onReload,
	}
	m.mu.Unlock()
	return nil
}

// RegisterJSON 注册 JSON 配置
// target 是指向配置结构体的指针
func (m *Manager) RegisterJSON(filename string, target interface{}, onReload func()) {
	m.mu.Lock()
	m.entries[filename] = &ConfigEntry{
		Filename:   filename,
		Type:       EntryTypeJSON,
		JSONTarget: target,
		OnReload:   onReload,
	}
	m.mu.Unlock()
}

// LoadAll 加载所有已注册的配置
func (m *Manager) LoadAll() error {
	m.mu.RLock()
	entries := make([]*ConfigEntry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	m.mu.RUnlock()

	for _, e := range entries {
		if err := m.loadEntry(e); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) loadEntry(e *ConfigEntry) error {
	info, err := os.Stat(e.Filename)
	if err != nil {
		return err
	}
	e.ModTime = info.ModTime()

	switch e.Type {
	case EntryTypeRecordFile:
		return e.RecordFile.Read(e.Filename)
	case EntryTypeJSON:
		return LoadJSON(e.Filename, e.JSONTarget)
	}
	return nil
}

// Get 获取配置条目
func (m *Manager) Get(filename string) *ConfigEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entries[filename]
}

// StartWatch 启动文件监控，定期检查文件变更并自动重载
func (m *Manager) StartWatch(interval time.Duration) {
	m.mu.Lock()
	if m.watching {
		m.mu.Unlock()
		return
	}
	m.watching = true
	m.stopCh = make(chan struct{})
	m.mu.Unlock()

	go m.watchLoop(interval)
}

// StopWatch 停止文件监控
func (m *Manager) StopWatch() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.watching {
		return
	}
	m.watching = false
	close(m.stopCh)
}

func (m *Manager) watchLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkReload()
		}
	}
}

func (m *Manager) checkReload() {
	m.mu.RLock()
	entries := make([]*ConfigEntry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	m.mu.RUnlock()

	for _, e := range entries {
		info, err := os.Stat(e.Filename)
		if err != nil {
			continue
		}
		if info.ModTime().After(e.ModTime) {
			log.Info("[config] reloading %s", e.Filename)
			if err := m.loadEntry(e); err != nil {
				log.Error("[config] reload %s failed: %v", e.Filename, err)
				continue
			}
			if e.OnReload != nil {
				e.OnReload()
			}
		}
	}
}
