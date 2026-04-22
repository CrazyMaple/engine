package actor

// Behavior 行为函数
type Behavior func(ctx Context)

// BehaviorStack 行为栈
type BehaviorStack struct {
	behaviors []Behavior
}

// NewBehaviorStack 创建行为栈
func NewBehaviorStack(initial Behavior) *BehaviorStack {
	return &BehaviorStack{
		behaviors: []Behavior{initial},
	}
}

// Receive 执行当前行为
func (bs *BehaviorStack) Receive(ctx Context) {
	if len(bs.behaviors) > 0 {
		bs.behaviors[len(bs.behaviors)-1](ctx)
	}
}

// Become 替换当前行为
func (bs *BehaviorStack) Become(behavior Behavior) {
	if len(bs.behaviors) > 0 {
		bs.behaviors[len(bs.behaviors)-1] = behavior
	} else {
		bs.behaviors = append(bs.behaviors, behavior)
	}
}

// BecomeStacked 压入新行为
func (bs *BehaviorStack) BecomeStacked(behavior Behavior) {
	bs.behaviors = append(bs.behaviors, behavior)
}

// UnbecomeStacked 弹出当前行为
func (bs *BehaviorStack) UnbecomeStacked() {
	if len(bs.behaviors) > 1 {
		bs.behaviors = bs.behaviors[:len(bs.behaviors)-1]
	}
}

// Clear 清空行为栈
func (bs *BehaviorStack) Clear(initial Behavior) {
	bs.behaviors = []Behavior{initial}
}

// Peek 查看当前行为
func (bs *BehaviorStack) Peek() Behavior {
	if len(bs.behaviors) > 0 {
		return bs.behaviors[len(bs.behaviors)-1]
	}
	return nil
}
