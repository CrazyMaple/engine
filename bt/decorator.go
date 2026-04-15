package bt

// --- Inverter 取反装饰器 ---

type inverterNode struct {
	baseNode
	child Node
}

// Inverter 创建取反装饰器
// Success → Failure，Failure → Success，Running → Running
func Inverter(child Node) Node {
	return &inverterNode{
		baseNode: baseNode{name: "Inverter"},
		child:    child,
	}
}

func (n *inverterNode) Tick(bb *Blackboard) Status {
	status := n.child.Tick(bb)
	switch status {
	case Success:
		return Failure
	case Failure:
		return Success
	default:
		return Running
	}
}

func (n *inverterNode) Reset() { n.child.Reset() }

// --- Repeater 重复装饰器 ---

type repeaterNode struct {
	baseNode
	child    Node
	maxTimes int // -1 表示无限重复
	count    int
}

// Repeater 创建重复装饰器
// maxTimes: 重复次数，-1 为无限重复
func Repeater(maxTimes int, child Node) Node {
	return &repeaterNode{
		baseNode: baseNode{name: "Repeater"},
		child:    child,
		maxTimes: maxTimes,
	}
}

func (n *repeaterNode) Tick(bb *Blackboard) Status {
	if n.maxTimes >= 0 && n.count >= n.maxTimes {
		return Success
	}

	status := n.child.Tick(bb)
	if status == Running {
		return Running
	}

	n.count++
	n.child.Reset()

	if n.maxTimes >= 0 && n.count >= n.maxTimes {
		return Success
	}
	return Running
}

func (n *repeaterNode) Reset() {
	n.count = 0
	n.child.Reset()
}

// --- Limiter 限次装饰器 ---

type limiterNode struct {
	baseNode
	child    Node
	maxTimes int
	count    int
}

// Limiter 创建限次装饰器，超过次数后直接返回 Failure
func Limiter(maxTimes int, child Node) Node {
	return &limiterNode{
		baseNode: baseNode{name: "Limiter"},
		child:    child,
		maxTimes: maxTimes,
	}
}

func (n *limiterNode) Tick(bb *Blackboard) Status {
	if n.count >= n.maxTimes {
		return Failure
	}
	n.count++
	return n.child.Tick(bb)
}

func (n *limiterNode) Reset() {
	n.count = 0
	n.child.Reset()
}

// --- Succeeder 始终成功装饰器 ---

type succeederNode struct {
	baseNode
	child Node
}

// Succeeder 无论子节点结果如何，始终返回 Success（Running 除外）
func Succeeder(child Node) Node {
	return &succeederNode{
		baseNode: baseNode{name: "Succeeder"},
		child:    child,
	}
}

func (n *succeederNode) Tick(bb *Blackboard) Status {
	status := n.child.Tick(bb)
	if status == Running {
		return Running
	}
	return Success
}

func (n *succeederNode) Reset() { n.child.Reset() }

// --- UntilFail 持续执行直到失败 ---

type untilFailNode struct {
	baseNode
	child Node
}

// UntilFail 持续执行子节点直到返回 Failure，然后返回 Success
func UntilFail(child Node) Node {
	return &untilFailNode{
		baseNode: baseNode{name: "UntilFail"},
		child:    child,
	}
}

func (n *untilFailNode) Tick(bb *Blackboard) Status {
	status := n.child.Tick(bb)
	if status == Failure {
		return Success
	}
	return Running
}

func (n *untilFailNode) Reset() { n.child.Reset() }
