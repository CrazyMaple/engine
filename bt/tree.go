package bt

import (
	"encoding/json"
	"fmt"
)

// Tree 行为树，包装根节点并管理黑板
type Tree struct {
	root       Node
	blackboard *Blackboard
}

// NewTree 创建行为树
func NewTree(root Node) *Tree {
	return &Tree{
		root:       root,
		blackboard: NewBlackboard(),
	}
}

// Tick 执行一次行为树
func (t *Tree) Tick() Status {
	return t.root.Tick(t.blackboard)
}

// Reset 重置行为树所有节点状态
func (t *Tree) Reset() {
	t.root.Reset()
}

// Blackboard 获取黑板引用
func (t *Tree) Blackboard() *Blackboard {
	return t.blackboard
}

// Root 获取根节点
func (t *Tree) Root() Node {
	return t.root
}

// --- JSON 配置加载 ---

// ActionRegistry 命名动作/条件注册表，供 JSON 加载使用
type ActionRegistry struct {
	actions    map[string]ActionFunc
	conditions map[string]ConditionFunc
}

// NewActionRegistry 创建注册表
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{
		actions:    make(map[string]ActionFunc),
		conditions: make(map[string]ConditionFunc),
	}
}

// RegisterAction 注册命名动作
func (r *ActionRegistry) RegisterAction(name string, fn ActionFunc) {
	r.actions[name] = fn
}

// RegisterCondition 注册命名条件
func (r *ActionRegistry) RegisterCondition(name string, fn ConditionFunc) {
	r.conditions[name] = fn
}

// nodeJSON 行为树 JSON 配置格式
type nodeJSON struct {
	Type     string     `json:"type"`     // sequence, selector, parallel, inverter, repeater, limiter, action, condition
	Name     string     `json:"name"`     // 节点名称
	Children []nodeJSON `json:"children"` // 子节点
	// 装饰器参数
	MaxTimes int    `json:"max_times,omitempty"`
	Policy   string `json:"policy,omitempty"` // "require_all", "require_one"
}

// LoadTreeFromJSON 从 JSON 配置加载行为树
func LoadTreeFromJSON(data []byte, registry *ActionRegistry) (*Tree, error) {
	var root nodeJSON
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	node, err := buildNode(root, registry)
	if err != nil {
		return nil, err
	}

	return NewTree(node), nil
}

func buildNode(j nodeJSON, reg *ActionRegistry) (Node, error) {
	switch j.Type {
	case "sequence":
		children, err := buildChildren(j.Children, reg)
		if err != nil {
			return nil, err
		}
		return Sequence(children...), nil

	case "selector":
		children, err := buildChildren(j.Children, reg)
		if err != nil {
			return nil, err
		}
		return Selector(children...), nil

	case "parallel":
		children, err := buildChildren(j.Children, reg)
		if err != nil {
			return nil, err
		}
		policy := RequireAll
		if j.Policy == "require_one" {
			policy = RequireOne
		}
		return Parallel(policy, children...), nil

	case "inverter":
		if len(j.Children) != 1 {
			return nil, fmt.Errorf("inverter requires exactly 1 child, got %d", len(j.Children))
		}
		child, err := buildNode(j.Children[0], reg)
		if err != nil {
			return nil, err
		}
		return Inverter(child), nil

	case "repeater":
		if len(j.Children) != 1 {
			return nil, fmt.Errorf("repeater requires exactly 1 child, got %d", len(j.Children))
		}
		child, err := buildNode(j.Children[0], reg)
		if err != nil {
			return nil, err
		}
		return Repeater(j.MaxTimes, child), nil

	case "limiter":
		if len(j.Children) != 1 {
			return nil, fmt.Errorf("limiter requires exactly 1 child, got %d", len(j.Children))
		}
		child, err := buildNode(j.Children[0], reg)
		if err != nil {
			return nil, err
		}
		return Limiter(j.MaxTimes, child), nil

	case "action":
		fn, ok := reg.actions[j.Name]
		if !ok {
			return nil, fmt.Errorf("action %q not registered", j.Name)
		}
		return Action(j.Name, fn), nil

	case "condition":
		fn, ok := reg.conditions[j.Name]
		if !ok {
			return nil, fmt.Errorf("condition %q not registered", j.Name)
		}
		return Condition(j.Name, fn), nil

	default:
		return nil, fmt.Errorf("unknown node type: %q", j.Type)
	}
}

func buildChildren(children []nodeJSON, reg *ActionRegistry) ([]Node, error) {
	nodes := make([]Node, 0, len(children))
	for _, c := range children {
		node, err := buildNode(c, reg)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}
