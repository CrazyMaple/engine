package ecs

import (
	"sort"
	"sync"
	"time"
)

// System 系统接口，定义 ECS 中的行为逻辑
type System interface {
	// Name 系统名称（用于调试和日志）
	Name() string
	// Priority 优先级，数值越小越先执行
	Priority() int
	// Update 帧更新，deltaTime 为上一帧到当前帧的时间间隔
	Update(world *World, deltaTime time.Duration)
}

// SystemGroup 有序系统组，按优先级调度多个 System
type SystemGroup struct {
	systems    []System
	sorted     bool
	parallel   [][]System // 可并行的系统分组
	parallelOK bool
}

// NewSystemGroup 创建系统组
func NewSystemGroup() *SystemGroup {
	return &SystemGroup{}
}

// Add 添加系统
func (sg *SystemGroup) Add(systems ...System) {
	sg.systems = append(sg.systems, systems...)
	sg.sorted = false
	sg.parallelOK = false
}

// Remove 按名称移除系统
func (sg *SystemGroup) Remove(name string) {
	for i, s := range sg.systems {
		if s.Name() == name {
			sg.systems = append(sg.systems[:i], sg.systems[i+1:]...)
			sg.sorted = false
			sg.parallelOK = false
			return
		}
	}
}

// sort 按优先级排序
func (sg *SystemGroup) ensureSorted() {
	if sg.sorted {
		return
	}
	sort.Slice(sg.systems, func(i, j int) bool {
		return sg.systems[i].Priority() < sg.systems[j].Priority()
	})
	sg.sorted = true
}

// Update 按优先级顺序执行所有系统（串行）
func (sg *SystemGroup) Update(world *World, deltaTime time.Duration) {
	sg.ensureSorted()
	for _, s := range sg.systems {
		s.Update(world, deltaTime)
	}
}

// ParallelSystem 可标记是否支持并行执行的系统
type ParallelSystem interface {
	System
	// CanParallel 返回 true 表示此系统无数据依赖，可与同优先级系统并行
	CanParallel() bool
}

// UpdateParallel 并行调度无数据依赖的系统
// 同优先级且标记 CanParallel 的系统会并行执行
// 不同优先级之间严格串行
func (sg *SystemGroup) UpdateParallel(world *World, deltaTime time.Duration) {
	sg.ensureSorted()
	if !sg.parallelOK {
		sg.buildParallelGroups()
	}

	for _, group := range sg.parallel {
		if len(group) == 1 {
			group[0].Update(world, deltaTime)
			continue
		}

		// 同一优先级组内并行
		var wg sync.WaitGroup
		wg.Add(len(group))
		for _, s := range group {
			go func(sys System) {
				defer wg.Done()
				sys.Update(world, deltaTime)
			}(s)
		}
		wg.Wait()
	}
}

// buildParallelGroups 按优先级构建并行分组
func (sg *SystemGroup) buildParallelGroups() {
	sg.parallel = nil
	if len(sg.systems) == 0 {
		sg.parallelOK = true
		return
	}

	var currentGroup []System
	currentPriority := sg.systems[0].Priority()

	for _, s := range sg.systems {
		if s.Priority() != currentPriority {
			sg.parallel = append(sg.parallel, currentGroup)
			currentGroup = nil
			currentPriority = s.Priority()
		}

		// 检查是否支持并行
		if ps, ok := s.(ParallelSystem); ok && ps.CanParallel() {
			currentGroup = append(currentGroup, s)
		} else {
			// 不支持并行的系统独占一组
			if len(currentGroup) > 0 {
				sg.parallel = append(sg.parallel, currentGroup)
				currentGroup = nil
			}
			sg.parallel = append(sg.parallel, []System{s})
		}
	}

	if len(currentGroup) > 0 {
		sg.parallel = append(sg.parallel, currentGroup)
	}
	sg.parallelOK = true
}

// Systems 返回系统列表（按优先级排序）
func (sg *SystemGroup) Systems() []System {
	sg.ensureSorted()
	result := make([]System, len(sg.systems))
	copy(result, sg.systems)
	return result
}

// Count 系统数量
func (sg *SystemGroup) Count() int {
	return len(sg.systems)
}
