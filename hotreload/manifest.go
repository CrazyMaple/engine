package hotreload

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// --- 插件清单文件与标准化生命周期 ---

// PluginManifest 插件清单（plugin.yaml）
type PluginManifest struct {
	Name         string            `yaml:"name" json:"name"`                   // 插件唯一名称
	Version      string            `yaml:"version" json:"version"`             // 语义化版本 (semver)
	Description  string            `yaml:"description" json:"description"`     // 插件说明
	Author       string            `yaml:"author" json:"author"`               // 作者
	EngineMinVer string            `yaml:"engine_min_version" json:"engine_min_version"` // 兼容引擎最低版本
	Dependencies []PluginDep       `yaml:"dependencies" json:"dependencies"`   // 依赖其他插件
	Entrypoint   string            `yaml:"entrypoint" json:"entrypoint"`       // 入口文件 (.so 路径，相对于插件目录)
	Config       map[string]string `yaml:"config" json:"config"`               // 默认配置 KV
}

// PluginDep 插件依赖声明
type PluginDep struct {
	Name       string `yaml:"name" json:"name"`               // 依赖插件名称
	MinVersion string `yaml:"min_version" json:"min_version"` // 最低版本
}

// LoadManifest 从 plugin.yaml 文件加载清单
func LoadManifest(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if m.Name == "" {
		return nil, fmt.Errorf("manifest missing 'name' field")
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest missing 'version' field")
	}

	return &m, nil
}

// SaveManifest 保存清单到 plugin.yaml
func SaveManifest(path string, m *PluginManifest) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// --- 依赖解析 ---

// DependencyResolver 插件依赖解析器
type DependencyResolver struct {
	manifests map[string]*PluginManifest
}

// NewDependencyResolver 创建依赖解析器
func NewDependencyResolver() *DependencyResolver {
	return &DependencyResolver{
		manifests: make(map[string]*PluginManifest),
	}
}

// Add 添加插件清单
func (r *DependencyResolver) Add(m *PluginManifest) {
	r.manifests[m.Name] = m
}

// Resolve 解析加载顺序（拓扑排序），返回按依赖顺序排列的插件名列表
func (r *DependencyResolver) Resolve() ([]string, error) {
	// 构建邻接表和入度
	inDegree := make(map[string]int)
	graph := make(map[string][]string) // dep → dependents

	for name := range r.manifests {
		inDegree[name] = 0
	}

	for name, m := range r.manifests {
		for _, dep := range m.Dependencies {
			if _, ok := r.manifests[dep.Name]; !ok {
				return nil, fmt.Errorf("plugin %q depends on %q which is not available", name, dep.Name)
			}
			graph[dep.Name] = append(graph[dep.Name], name)
			inDegree[name]++
		}
	}

	// Kahn 拓扑排序
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue) // 确定性排序

	var order []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		for _, dependent := range graph[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
				sort.Strings(queue)
			}
		}
	}

	if len(order) != len(r.manifests) {
		return nil, fmt.Errorf("circular dependency detected among plugins")
	}

	return order, nil
}

// --- 插件隔离与生命周期钩子 ---

// LifecyclePhase 生命周期阶段
type LifecyclePhase string

const (
	PhaseInit        LifecyclePhase = "init"
	PhaseStart       LifecyclePhase = "start"
	PhaseHealthCheck LifecyclePhase = "healthcheck"
	PhaseStop        LifecyclePhase = "stop"
	PhaseCleanup     LifecyclePhase = "cleanup"
)

// LifecyclePlugin 标准化生命周期插件接口
// 在原有 Plugin 接口基础上扩展 Init/Start/HealthCheck/Stop/Cleanup 五阶段
type LifecyclePlugin interface {
	Plugin

	// Init 初始化阶段 — 读取配置、分配资源，但不启动业务逻辑
	Init(cfg map[string]string) error

	// Start 启动阶段 — 开始处理业务（在所有依赖插件 Start 之后调用）
	Start() error

	// HealthCheck 健康检查 — 定期调用，返回 nil 表示健康
	HealthCheck() error

	// Stop 停止阶段 — 停止接收新请求，完成正在处理的请求
	Stop() error

	// Cleanup 清理阶段 — 释放所有资源（在所有依赖本插件的插件 Cleanup 之后调用）
	Cleanup() error
}

// IsolatedContext 插件隔离上下文 — 独立的日志命名空间和指标前缀
type IsolatedContext struct {
	PluginName   string            // 插件名
	LogPrefix    string            // 日志前缀，如 "[plugin:my-plugin]"
	MetricPrefix string            // 指标前缀，如 "plugin_my_plugin_"
	Config       map[string]string // 插件配置
}

// NewIsolatedContext 创建隔离上下文
func NewIsolatedContext(name string, cfg map[string]string) *IsolatedContext {
	safeName := strings.ReplaceAll(name, "-", "_")
	return &IsolatedContext{
		PluginName:   name,
		LogPrefix:    fmt.Sprintf("[plugin:%s]", name),
		MetricPrefix: fmt.Sprintf("plugin_%s_", safeName),
		Config:       cfg,
	}
}

// --- 标准化插件管理器 ---

// StandardManager 标准化插件管理器（在 Manager 基础上增加清单、依赖解析、隔离）
type StandardManager struct {
	*Manager
	mu        sync.RWMutex
	manifests map[string]*PluginManifest
	contexts  map[string]*IsolatedContext
	loader    *GoPluginLoader
	resolver  *DependencyResolver
	order     []string // 加载顺序
	pluginDir string
}

// NewStandardManager 创建标准化插件管理器
func NewStandardManager(pluginDir string) *StandardManager {
	return &StandardManager{
		Manager:   NewManager(),
		manifests: make(map[string]*PluginManifest),
		contexts:  make(map[string]*IsolatedContext),
		loader:    NewGoPluginLoader(pluginDir),
		resolver:  NewDependencyResolver(),
		pluginDir: pluginDir,
	}
}

// Install 安装插件（从目录加载清单 + .so）
func (sm *StandardManager) Install(pluginPath string) error {
	// 读取清单
	manifestPath := filepath.Join(pluginPath, "plugin.yaml")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}

	sm.mu.Lock()
	sm.manifests[manifest.Name] = manifest
	sm.resolver.Add(manifest)
	sm.mu.Unlock()

	// 加载 .so
	soPath := filepath.Join(pluginPath, manifest.Entrypoint)
	if manifest.Entrypoint == "" {
		soPath = filepath.Join(pluginPath, manifest.Name+".so")
	}

	p, err := sm.loader.LoadFromFile(soPath)
	if err != nil {
		return fmt.Errorf("install: load plugin: %w", err)
	}

	// 创建隔离上下文
	ctx := NewIsolatedContext(manifest.Name, manifest.Config)
	sm.mu.Lock()
	sm.contexts[manifest.Name] = ctx
	sm.mu.Unlock()

	// 如果是 LifecyclePlugin，执行 Init
	if lp, ok := p.(LifecyclePlugin); ok {
		if err := lp.Init(manifest.Config); err != nil {
			return fmt.Errorf("install: init %s: %w", manifest.Name, err)
		}
	}

	return sm.Manager.Load(p)
}

// Remove 卸载插件
func (sm *StandardManager) Remove(name string) error {
	sm.mu.Lock()
	delete(sm.manifests, name)
	delete(sm.contexts, name)
	sm.mu.Unlock()

	// 如果是 LifecyclePlugin，执行 Stop + Cleanup
	p, ok := sm.Manager.Get(name)
	if ok {
		if lp, ok := p.(LifecyclePlugin); ok {
			lp.Stop()
			lp.Cleanup()
		}
	}

	return sm.Manager.Unload(name)
}

// StartAll 按依赖顺序启动所有已安装的插件
func (sm *StandardManager) StartAll() error {
	sm.mu.Lock()
	order, err := sm.resolver.Resolve()
	if err != nil {
		sm.mu.Unlock()
		return fmt.Errorf("resolve dependencies: %w", err)
	}
	sm.order = order
	sm.mu.Unlock()

	for _, name := range order {
		p, ok := sm.Manager.Get(name)
		if !ok {
			continue
		}
		if lp, ok := p.(LifecyclePlugin); ok {
			if err := lp.Start(); err != nil {
				return fmt.Errorf("start plugin %s: %w", name, err)
			}
		}
	}
	return nil
}

// StopAll 按依赖逆序停止所有插件
func (sm *StandardManager) StopAll() {
	sm.mu.RLock()
	order := sm.order
	sm.mu.RUnlock()

	// 逆序停止
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		p, ok := sm.Manager.Get(name)
		if !ok {
			continue
		}
		if lp, ok := p.(LifecyclePlugin); ok {
			lp.Stop()
			lp.Cleanup()
		}
	}
}

// ListInstalled 列出已安装插件（含清单信息）
func (sm *StandardManager) ListInstalled() []*PluginManifest {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]*PluginManifest, 0, len(sm.manifests))
	for _, m := range sm.manifests {
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// GetContext 获取插件的隔离上下文
func (sm *StandardManager) GetContext(name string) (*IsolatedContext, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ctx, ok := sm.contexts[name]
	return ctx, ok
}

// ScanAndInstall 扫描目录下所有含 plugin.yaml 的子目录并安装
func (sm *StandardManager) ScanAndInstall() (int, error) {
	entries, err := os.ReadDir(sm.pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(sm.pluginDir, entry.Name(), "plugin.yaml")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue
		}
		if err := sm.Install(filepath.Join(sm.pluginDir, entry.Name())); err != nil {
			return count, fmt.Errorf("install %s: %w", entry.Name(), err)
		}
		count++
	}

	return count, nil
}
