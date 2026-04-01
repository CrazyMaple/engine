package ecs

// Entity 实体，作为组件的容器
type Entity struct {
	ID         string
	components map[string]Component
}

// NewEntity 创建实体
func NewEntity(id string) *Entity {
	return &Entity{
		ID:         id,
		components: make(map[string]Component),
	}
}

// Add 添加组件，同类型组件会被覆盖
func (e *Entity) Add(c Component) {
	e.components[c.ComponentType()] = c
}

// Get 获取组件
func (e *Entity) Get(typeName string) (Component, bool) {
	c, ok := e.components[typeName]
	return c, ok
}

// Remove 移除组件
func (e *Entity) Remove(typeName string) {
	delete(e.components, typeName)
}

// Has 检查是否有某组件
func (e *Entity) Has(typeName string) bool {
	_, ok := e.components[typeName]
	return ok
}

// All 获取所有组件
func (e *Entity) All() []Component {
	result := make([]Component, 0, len(e.components))
	for _, c := range e.components {
		result = append(result, c)
	}
	return result
}

// GetPosition 快捷获取 Position 组件
func (e *Entity) GetPosition() *Position {
	if c, ok := e.Get("Position"); ok {
		return c.(*Position)
	}
	return nil
}

// GetHealth 快捷获取 Health 组件
func (e *Entity) GetHealth() *Health {
	if c, ok := e.Get("Health"); ok {
		return c.(*Health)
	}
	return nil
}
