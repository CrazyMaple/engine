package grain

import (
	"sync"
	"time"

	"engine/actor"
)

// DefaultTTL 默认的 Grain 超时时间
const DefaultTTL = 10 * time.Minute

// Kind 虚拟 Actor 类型定义
type Kind struct {
	// Name 类型名称
	Name string

	// Props Actor 配置
	Props *actor.Props

	// TTL 空闲超时时间，超时后自动去激活
	TTL time.Duration
}

// NewKind 创建 Kind
func NewKind(name string, producer actor.Producer) *Kind {
	return &Kind{
		Name:  name,
		Props: actor.PropsFromProducer(producer),
		TTL:   DefaultTTL,
	}
}

// WithTTL 设置超时时间
func (k *Kind) WithTTL(ttl time.Duration) *Kind {
	k.TTL = ttl
	return k
}

// WithProps 直接设置 Props
func (k *Kind) WithProps(props *actor.Props) *Kind {
	k.Props = props
	return k
}

// KindRegistry Kind 注册表
type KindRegistry struct {
	kinds map[string]*Kind
	mu    sync.RWMutex
}

// NewKindRegistry 创建 Kind 注册表
func NewKindRegistry() *KindRegistry {
	return &KindRegistry{
		kinds: make(map[string]*Kind),
	}
}

// Register 注册 Kind
func (kr *KindRegistry) Register(kind *Kind) {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	kr.kinds[kind.Name] = kind
}

// Get 获取 Kind
func (kr *KindRegistry) Get(name string) (*Kind, bool) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	k, ok := kr.kinds[name]
	return k, ok
}

// GetNames 获取所有已注册的 Kind 名称
func (kr *KindRegistry) GetNames() []string {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	names := make([]string, 0, len(kr.kinds))
	for name := range kr.kinds {
		names = append(names, name)
	}
	return names
}
