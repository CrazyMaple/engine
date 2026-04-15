package bt

// ActionFunc 动作函数类型
type ActionFunc func(bb *Blackboard) Status

// ConditionFunc 条件函数类型
type ConditionFunc func(bb *Blackboard) bool

// actionNode 动作叶子节点
type actionNode struct {
	baseNode
	fn ActionFunc
}

// Action 创建动作节点
func Action(name string, fn ActionFunc) Node {
	return &actionNode{
		baseNode: baseNode{name: name},
		fn:       fn,
	}
}

func (n *actionNode) Tick(bb *Blackboard) Status {
	return n.fn(bb)
}

// conditionNode 条件叶子节点
type conditionNode struct {
	baseNode
	fn ConditionFunc
}

// Condition 创建条件节点，返回 true→Success，false→Failure
func Condition(name string, fn ConditionFunc) Node {
	return &conditionNode{
		baseNode: baseNode{name: name},
		fn:       fn,
	}
}

func (n *conditionNode) Tick(bb *Blackboard) Status {
	if n.fn(bb) {
		return Success
	}
	return Failure
}
