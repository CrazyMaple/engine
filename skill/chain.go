package skill

import (
	"time"
)

// ChainNode DAG 节点：一次连锁派生的抽象节点
// 可选分支：按条件走 Next 中不同路径，支持并行（同一节点多条 Next 全部触发）。
type ChainNode struct {
	ID        string      // 节点 ID（用于调试 / 图可视化）
	SkillID   string      // 触发的技能 ID（可选）
	BuffID    string      // 施加的 Buff ID（可选）
	Effects   []string    // 额外执行的效果 ID 列表
	Delay     time.Duration
	Condition *Condition  // 进入本节点的前置条件（nil = 必进）
	// Next 后续节点（若长度 >1 表示并行分支，全部满足条件的节点都会执行）
	Next []*ChainNode
}

// ChainPlan 一个完整的技能连锁计划
type ChainPlan struct {
	Root *ChainNode
}

// Walk 展开 DAG 得到派生动作列表
// 从 root 出发，按 Condition 过滤、Delay 累加，收集全部可执行 ChainAction。
// 不做实际的循环检测，使用者应保证 DAG 无环。
func (p *ChainPlan) Walk(ctx *TriggerContext, start time.Time) []ChainAction {
	if p == nil || p.Root == nil {
		return nil
	}
	var out []ChainAction
	walkNode(p.Root, ctx, start, &out)
	return out
}

func walkNode(node *ChainNode, ctx *TriggerContext, t time.Time, out *[]ChainAction) {
	if node == nil {
		return
	}
	// 条件过滤
	if node.Condition != nil && !node.Condition.Evaluate(ctx) {
		return
	}
	execAt := t.Add(node.Delay)
	if node.SkillID != "" || node.BuffID != "" || len(node.Effects) > 0 {
		*out = append(*out, ChainAction{
			SkillID:   node.SkillID,
			TargetID:  ctx.TargetID,
			BuffID:    node.BuffID,
			Effects:   node.Effects,
			ExecuteAt: execAt,
			Source:    node.ID,
		})
	}
	// 后续节点：基于 execAt 继续展开
	for _, nxt := range node.Next {
		walkNode(nxt, ctx, execAt, out)
	}
}

// ChainScheduler 链式动作调度器：按 ExecuteAt 排序并按时间到期逐条弹出
type ChainScheduler struct {
	pending []ChainAction
}

// NewChainScheduler 创建调度器
func NewChainScheduler() *ChainScheduler {
	return &ChainScheduler{}
}

// Push 加入待执行动作
func (s *ChainScheduler) Push(actions ...ChainAction) {
	s.pending = append(s.pending, actions...)
	// 小数据量下插入排序足以
	for i := 1; i < len(s.pending); i++ {
		for j := i; j > 0 && s.pending[j-1].ExecuteAt.After(s.pending[j].ExecuteAt); j-- {
			s.pending[j-1], s.pending[j] = s.pending[j], s.pending[j-1]
		}
	}
}

// PopDue 返回所有到期动作（ExecuteAt <= now），并从队列移除
func (s *ChainScheduler) PopDue(now time.Time) []ChainAction {
	if len(s.pending) == 0 {
		return nil
	}
	cut := 0
	for cut < len(s.pending) && !s.pending[cut].ExecuteAt.After(now) {
		cut++
	}
	if cut == 0 {
		return nil
	}
	due := append([]ChainAction(nil), s.pending[:cut]...)
	s.pending = append(s.pending[:0], s.pending[cut:]...)
	return due
}

// Pending 当前待执行动作数
func (s *ChainScheduler) Pending() int { return len(s.pending) }

// Clear 清空所有待执行动作（打断用）
func (s *ChainScheduler) Clear() { s.pending = s.pending[:0] }

// CancelCaster 按施法者取消链式动作（会话被打断时调用）
// 注意：ChainAction 本身不携带 CasterID，调用者若需粒度更细应自行过滤。
// 此便利函数按 Source 前缀匹配移除。
func (s *ChainScheduler) CancelBySourcePrefix(prefix string) int {
	if prefix == "" || len(s.pending) == 0 {
		return 0
	}
	n := 0
	out := s.pending[:0]
	for _, a := range s.pending {
		if hasPrefix(a.Source, prefix) {
			n++
			continue
		}
		out = append(out, a)
	}
	s.pending = out
	return n
}

func hasPrefix(s, p string) bool {
	if len(s) < len(p) {
		return false
	}
	return s[:len(p)] == p
}
