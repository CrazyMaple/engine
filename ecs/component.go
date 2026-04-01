package ecs

// Component 组件接口，所有实体组件必须实现
type Component interface {
	ComponentType() string
}

// --- 内置游戏组件 ---

// Position 位置组件
type Position struct {
	X, Y, Z float32
}

func (p *Position) ComponentType() string { return "Position" }

// Rotation 旋转组件
type Rotation struct {
	Yaw, Pitch, Roll float32
}

func (r *Rotation) ComponentType() string { return "Rotation" }

// Health 生命值组件
type Health struct {
	Current int
	Max     int
}

func (h *Health) ComponentType() string { return "Health" }

// Movement 移动组件
type Movement struct {
	Speed    float32
	VelocityX float32
	VelocityY float32
	VelocityZ float32
}

func (m *Movement) ComponentType() string { return "Movement" }
