package bt

// --- Sequence 顺序节点 ---

// sequenceNode 顺序执行所有子节点
// 任一子节点 Failure → 返回 Failure
// 任一子节点 Running → 返回 Running
// 全部 Success → 返回 Success
type sequenceNode struct {
	baseNode
	children   []Node
	currentIdx int // 当前执行到的子节点索引（支持 Running 续跑）
}

// Sequence 创建顺序节点
func Sequence(children ...Node) Node {
	return &sequenceNode{
		baseNode: baseNode{name: "Sequence"},
		children: children,
	}
}

func (n *sequenceNode) Tick(bb *Blackboard) Status {
	for i := n.currentIdx; i < len(n.children); i++ {
		status := n.children[i].Tick(bb)
		switch status {
		case Failure:
			n.currentIdx = 0
			return Failure
		case Running:
			n.currentIdx = i
			return Running
		}
		// Success → 继续下一个
	}
	n.currentIdx = 0
	return Success
}

func (n *sequenceNode) Reset() {
	n.currentIdx = 0
	for _, c := range n.children {
		c.Reset()
	}
}

// --- Selector 选择节点 ---

// selectorNode 选择执行子节点（优先级选择）
// 任一子节点 Success → 返回 Success
// 任一子节点 Running → 返回 Running
// 全部 Failure → 返回 Failure
type selectorNode struct {
	baseNode
	children   []Node
	currentIdx int
}

// Selector 创建选择节点
func Selector(children ...Node) Node {
	return &selectorNode{
		baseNode: baseNode{name: "Selector"},
		children: children,
	}
}

func (n *selectorNode) Tick(bb *Blackboard) Status {
	for i := n.currentIdx; i < len(n.children); i++ {
		status := n.children[i].Tick(bb)
		switch status {
		case Success:
			n.currentIdx = 0
			return Success
		case Running:
			n.currentIdx = i
			return Running
		}
		// Failure → 尝试下一个
	}
	n.currentIdx = 0
	return Failure
}

func (n *selectorNode) Reset() {
	n.currentIdx = 0
	for _, c := range n.children {
		c.Reset()
	}
}

// --- Parallel 并行节点 ---

// ParallelPolicy 并行节点的成功策略
type ParallelPolicy int

const (
	// RequireAll 所有子节点 Success 才 Success
	RequireAll ParallelPolicy = iota
	// RequireOne 任一子节点 Success 即 Success
	RequireOne
)

// parallelNode 并行执行所有子节点
type parallelNode struct {
	baseNode
	children []Node
	policy   ParallelPolicy
}

// Parallel 创建并行节点
func Parallel(policy ParallelPolicy, children ...Node) Node {
	return &parallelNode{
		baseNode: baseNode{name: "Parallel"},
		children: children,
		policy:   policy,
	}
}

func (n *parallelNode) Tick(bb *Blackboard) Status {
	successCount := 0
	failureCount := 0

	for _, child := range n.children {
		status := child.Tick(bb)
		switch status {
		case Success:
			successCount++
		case Failure:
			failureCount++
		}
	}

	switch n.policy {
	case RequireOne:
		if successCount > 0 {
			return Success
		}
		if failureCount == len(n.children) {
			return Failure
		}
	case RequireAll:
		if successCount == len(n.children) {
			return Success
		}
		if failureCount > 0 {
			return Failure
		}
	}

	return Running
}

func (n *parallelNode) Reset() {
	for _, c := range n.children {
		c.Reset()
	}
}
