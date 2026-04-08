package cluster

import "time"

// SplitBrainDetectedEvent 脑裂检测事件，发布到 EventStream
type SplitBrainDetectedEvent struct {
	ReachableMembers   []*Member
	UnreachableMembers []*Member
	DetectedAt         time.Time
}

// SplitBrainResolvedEvent 脑裂解决事件
type SplitBrainResolvedEvent struct {
	Decision   ResolverDecision
	ResolvedAt time.Time
}
