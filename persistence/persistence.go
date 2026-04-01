package persistence

import "context"

// Persistable Actor 持久化接口
// 需要持久化的 Actor 应实现此接口
type Persistable interface {
	// PersistenceID 返回持久化唯一标识
	PersistenceID() string
	// GetState 获取当前状态用于保存
	GetState() interface{}
	// SetState 从存储恢复状态
	SetState(state interface{}) error
}

// Storage 存储后端接口
type Storage interface {
	// Save 保存状态
	Save(ctx context.Context, id string, state interface{}) error
	// Load 加载状态到 target（target 是指针）
	Load(ctx context.Context, id string, target interface{}) error
	// Delete 删除状态
	Delete(ctx context.Context, id string) error
}
