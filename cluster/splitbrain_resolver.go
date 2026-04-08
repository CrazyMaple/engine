package cluster

import "sort"

// ResolverDecision 脑裂解决决策
type ResolverDecision int

const (
	// DecisionKeepRunning 当前分区继续运行
	DecisionKeepRunning ResolverDecision = iota
	// DecisionShutdown 当前分区应关闭
	DecisionShutdown
)

func (d ResolverDecision) String() string {
	switch d {
	case DecisionKeepRunning:
		return "KeepRunning"
	case DecisionShutdown:
		return "Shutdown"
	default:
		return "Unknown"
	}
}

// SplitBrainResolver 脑裂解决策略接口
type SplitBrainResolver interface {
	// Resolve 根据当前分区信息决定是否继续运行
	Resolve(ctx ResolverContext) ResolverDecision
}

// ResolverContext 提供给解决策略的上下文信息
type ResolverContext struct {
	// Self 本节点信息
	Self *Member
	// Reachable 当前可达成员列表（含自己）
	Reachable []*Member
	// AllKnown 所有已知成员（含不可达）
	AllKnown []*Member
}

// KeepOldestResolver 保留包含最老节点（字典序最小 ID）的分区
// 确定性选择：所有节点对"最老"的判定一致
type KeepOldestResolver struct{}

func (r *KeepOldestResolver) Resolve(ctx ResolverContext) ResolverDecision {
	if len(ctx.AllKnown) == 0 || len(ctx.Reachable) == 0 {
		return DecisionShutdown
	}

	// 找到全局最老节点（字典序最小 ID）
	oldestID := ctx.AllKnown[0].Id
	for _, m := range ctx.AllKnown[1:] {
		if m.Id < oldestID {
			oldestID = m.Id
		}
	}

	// 检查最老节点是否在可达集合中
	for _, m := range ctx.Reachable {
		if m.Id == oldestID {
			return DecisionKeepRunning
		}
	}

	return DecisionShutdown
}

// KeepMajorityResolver 保留成员数更多的分区
// 平局时保留包含字典序最小 ID 的分区
type KeepMajorityResolver struct{}

func (r *KeepMajorityResolver) Resolve(ctx ResolverContext) ResolverDecision {
	total := len(ctx.AllKnown)
	reachable := len(ctx.Reachable)

	if total == 0 {
		return DecisionShutdown
	}

	// 严格多数
	if reachable > total/2 {
		return DecisionKeepRunning
	}

	// 严格少数
	if reachable < (total+1)/2 {
		return DecisionShutdown
	}

	// 平局（偶数个节点恰好对半分）：使用确定性的 tie-breaker
	// 保留包含字典序最小 ID 的分区
	allIDs := make([]string, 0, total)
	for _, m := range ctx.AllKnown {
		allIDs = append(allIDs, m.Id)
	}
	sort.Strings(allIDs)
	smallestID := allIDs[0]

	for _, m := range ctx.Reachable {
		if m.Id == smallestID {
			return DecisionKeepRunning
		}
	}

	return DecisionShutdown
}

// ShutdownAllResolver 全部关闭策略（最安全，避免数据不一致）
type ShutdownAllResolver struct{}

func (r *ShutdownAllResolver) Resolve(_ ResolverContext) ResolverDecision {
	return DecisionShutdown
}
