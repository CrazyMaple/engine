package ecs

// World 实体注册表，用于场景内实体管理和查询
// 非线程安全，应在单个 Actor 内部使用
type World struct {
	entities map[string]*Entity
}

// NewWorld 创建实体世界
func NewWorld() *World {
	return &World{
		entities: make(map[string]*Entity),
	}
}

// Add 添加实体
func (w *World) Add(e *Entity) {
	w.entities[e.ID] = e
}

// Remove 移除实体
func (w *World) Remove(id string) {
	delete(w.entities, id)
}

// Get 获取实体
func (w *World) Get(id string) (*Entity, bool) {
	e, ok := w.entities[id]
	return e, ok
}

// Count 实体数量
func (w *World) Count() int {
	return len(w.entities)
}

// Query 查询包含指定组件类型的所有实体
func (w *World) Query(componentType string) []*Entity {
	result := make([]*Entity, 0)
	for _, e := range w.entities {
		if e.Has(componentType) {
			result = append(result, e)
		}
	}
	return result
}

// QueryMulti 查询同时包含多个组件类型的所有实体
func (w *World) QueryMulti(componentTypes ...string) []*Entity {
	result := make([]*Entity, 0)
	for _, e := range w.entities {
		hasAll := true
		for _, ct := range componentTypes {
			if !e.Has(ct) {
				hasAll = false
				break
			}
		}
		if hasAll {
			result = append(result, e)
		}
	}
	return result
}

// Each 遍历所有实体
func (w *World) Each(fn func(e *Entity)) {
	for _, e := range w.entities {
		fn(e)
	}
}
