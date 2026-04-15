package bt

// Status 行为树节点执行状态
type Status int

const (
	// Running 节点正在执行中，需要下一帧继续
	Running Status = iota
	// Success 节点执行成功
	Success
	// Failure 节点执行失败
	Failure
)

func (s Status) String() string {
	switch s {
	case Running:
		return "Running"
	case Success:
		return "Success"
	case Failure:
		return "Failure"
	default:
		return "Unknown"
	}
}

// Node 行为树节点接口
type Node interface {
	// Tick 执行一次节点逻辑
	Tick(bb *Blackboard) Status
	// Reset 重置节点状态（用于重新开始）
	Reset()
	// Name 节点名称（调试用）
	Name() string
}

// baseNode 基础节点（提供默认 Name/Reset 实现）
type baseNode struct {
	name string
}

func (n *baseNode) Name() string { return n.name }
func (n *baseNode) Reset()       {}
