package room

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Matcher 匹配器接口
type Matcher interface {
	// AddPlayer 添加玩家到匹配队列
	AddPlayer(player PlayerInfo)
	// RemovePlayer 从匹配队列移除玩家
	RemovePlayer(playerID string)
	// Match 尝试匹配一组玩家，返回匹配结果
	// 返回 nil 表示当前无法匹配
	Match() []PlayerInfo
	// QueueSize 当前队列中的玩家数
	QueueSize() int
}

// MatchResult 匹配结果
type MatchResult struct {
	Players   []PlayerInfo
	RoomType  string
	MatchedAt time.Time
}

// --- EloMatcher: 基于 ELO 分数范围匹配 ---

// EloMatcherConfig ELO 匹配器配置
type EloMatcherConfig struct {
	// PlayersPerMatch 每局玩家数
	PlayersPerMatch int
	// InitialRange 初始 ELO 匹配范围
	InitialRange float64
	// RangeExpansion 每次扩展的范围增量
	RangeExpansion float64
	// MaxRange 最大匹配范围
	MaxRange float64
	// ExpandInterval 匹配范围扩展间隔
	ExpandInterval time.Duration
}

// eloEntry 匹配队列条目
type eloEntry struct {
	Player   PlayerInfo
	JoinedAt time.Time
}

// EloMatcher 基于 ELO 分数匹配
type EloMatcher struct {
	config EloMatcherConfig
	queue  []eloEntry
	mu     sync.Mutex
}

// NewEloMatcher 创建 ELO 匹配器
func NewEloMatcher(config EloMatcherConfig) *EloMatcher {
	if config.PlayersPerMatch <= 0 {
		config.PlayersPerMatch = 2
	}
	if config.InitialRange <= 0 {
		config.InitialRange = 100
	}
	if config.MaxRange <= 0 {
		config.MaxRange = 500
	}
	if config.RangeExpansion <= 0 {
		config.RangeExpansion = 50
	}
	if config.ExpandInterval <= 0 {
		config.ExpandInterval = 10 * time.Second
	}

	return &EloMatcher{config: config}
}

func (m *EloMatcher) AddPlayer(player PlayerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = append(m.queue, eloEntry{Player: player, JoinedAt: time.Now()})
}

func (m *EloMatcher) RemovePlayer(playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.queue {
		if e.Player.PlayerID == playerID {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

func (m *EloMatcher) Match() []PlayerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.queue) < m.config.PlayersPerMatch {
		return nil
	}

	now := time.Now()

	// 对每个玩家，计算当前允许的匹配范围（随等待时间扩展）
	for i := 0; i < len(m.queue); i++ {
		anchor := m.queue[i]
		allowedRange := m.currentRange(anchor.JoinedAt, now)

		candidates := []int{i}
		for j := 0; j < len(m.queue); j++ {
			if i == j {
				continue
			}
			if math.Abs(anchor.Player.Rating-m.queue[j].Player.Rating) <= allowedRange {
				candidates = append(candidates, j)
			}
			if len(candidates) >= m.config.PlayersPerMatch {
				break
			}
		}

		if len(candidates) >= m.config.PlayersPerMatch {
			// 匹配成功，取出前 N 个
			result := make([]PlayerInfo, m.config.PlayersPerMatch)
			// 倒序移除避免索引错位
			matched := candidates[:m.config.PlayersPerMatch]
			for k, idx := range matched {
				result[k] = m.queue[idx].Player
			}
			// 从队列移除已匹配玩家（倒序）
			for k := len(matched) - 1; k >= 0; k-- {
				idx := matched[k]
				m.queue = append(m.queue[:idx], m.queue[idx+1:]...)
			}
			return result
		}
	}

	return nil
}

func (m *EloMatcher) QueueSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queue)
}

// currentRange 计算当前允许的 ELO 范围
func (m *EloMatcher) currentRange(joinedAt, now time.Time) float64 {
	waited := now.Sub(joinedAt)
	expansions := float64(waited) / float64(m.config.ExpandInterval)
	r := m.config.InitialRange + expansions*m.config.RangeExpansion
	if r > m.config.MaxRange {
		return m.config.MaxRange
	}
	return r
}

// --- QueueMatcher: 先到先得队列匹配 ---

// QueueMatcher 先到先得匹配器
type QueueMatcher struct {
	playersPerMatch int
	queue           []PlayerInfo
	mu              sync.Mutex
}

// NewQueueMatcher 创建队列匹配器
func NewQueueMatcher(playersPerMatch int) *QueueMatcher {
	if playersPerMatch <= 0 {
		playersPerMatch = 2
	}
	return &QueueMatcher{playersPerMatch: playersPerMatch}
}

func (m *QueueMatcher) AddPlayer(player PlayerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = append(m.queue, player)
}

func (m *QueueMatcher) RemovePlayer(playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.queue {
		if p.PlayerID == playerID {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

func (m *QueueMatcher) Match() []PlayerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.queue) < m.playersPerMatch {
		return nil
	}

	result := make([]PlayerInfo, m.playersPerMatch)
	copy(result, m.queue[:m.playersPerMatch])
	m.queue = m.queue[m.playersPerMatch:]
	return result
}

func (m *QueueMatcher) QueueSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queue)
}

// --- ConditionMatcher: 自定义条件匹配 ---

// MatchCondition 匹配条件判定函数
// 返回 true 表示两个玩家可以被匹配在一起
type MatchCondition func(a, b PlayerInfo) bool

// ConditionMatcher 自定义条件匹配器
type ConditionMatcher struct {
	playersPerMatch int
	condition       MatchCondition
	queue           []PlayerInfo
	mu              sync.Mutex
}

// NewConditionMatcher 创建条件匹配器
func NewConditionMatcher(playersPerMatch int, condition MatchCondition) *ConditionMatcher {
	if playersPerMatch <= 0 {
		playersPerMatch = 2
	}
	return &ConditionMatcher{
		playersPerMatch: playersPerMatch,
		condition:       condition,
	}
}

func (m *ConditionMatcher) AddPlayer(player PlayerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = append(m.queue, player)
}

func (m *ConditionMatcher) RemovePlayer(playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.queue {
		if p.PlayerID == playerID {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

func (m *ConditionMatcher) Match() []PlayerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.queue) < m.playersPerMatch {
		return nil
	}

	// 对每个玩家，找满足条件的匹配组
	for i := 0; i < len(m.queue); i++ {
		candidates := []int{i}
		for j := i + 1; j < len(m.queue); j++ {
			allMatch := true
			for _, ci := range candidates {
				if !m.condition(m.queue[ci], m.queue[j]) {
					allMatch = false
					break
				}
			}
			if allMatch {
				candidates = append(candidates, j)
			}
			if len(candidates) >= m.playersPerMatch {
				break
			}
		}

		if len(candidates) >= m.playersPerMatch {
			result := make([]PlayerInfo, m.playersPerMatch)
			matched := candidates[:m.playersPerMatch]
			for k, idx := range matched {
				result[k] = m.queue[idx]
			}
			for k := len(matched) - 1; k >= 0; k-- {
				idx := matched[k]
				m.queue = append(m.queue[:idx], m.queue[idx+1:]...)
			}
			return result
		}
	}

	return nil
}

func (m *ConditionMatcher) QueueSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queue)
}

// --- 匹配服务 ---

// MatchService 匹配服务，将匹配器与房间管理器关联
type MatchService struct {
	manager  *RoomManager
	matchers map[string]Matcher // roomType → matcher
	configs  map[string]RoomConfig
	stopCh   chan struct{}
	mu       sync.RWMutex
}

// NewMatchService 创建匹配服务
func NewMatchService(manager *RoomManager) *MatchService {
	return &MatchService{
		manager:  manager,
		matchers: make(map[string]Matcher),
		configs:  make(map[string]RoomConfig),
		stopCh:   make(chan struct{}),
	}
}

// RegisterMatcher 注册匹配器
func (s *MatchService) RegisterMatcher(roomType string, matcher Matcher, config RoomConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matchers[roomType] = matcher
	s.configs[roomType] = config
}

// EnqueuePlayer 将玩家加入匹配队列
func (s *MatchService) EnqueuePlayer(roomType string, player PlayerInfo) error {
	s.mu.RLock()
	matcher, ok := s.matchers[roomType]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no matcher registered for room type %q", roomType)
	}

	matcher.AddPlayer(player)
	return nil
}

// DequeuePlayer 将玩家从匹配队列移除
func (s *MatchService) DequeuePlayer(roomType string, playerID string) {
	s.mu.RLock()
	matcher, ok := s.matchers[roomType]
	s.mu.RUnlock()

	if ok {
		matcher.RemovePlayer(playerID)
	}
}

// Start 启动匹配循环
func (s *MatchService) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.tick()
			}
		}
	}()
}

// Stop 停止匹配服务
func (s *MatchService) Stop() {
	close(s.stopCh)
}

// tick 执行一轮匹配
func (s *MatchService) tick() {
	s.mu.RLock()
	types := make([]string, 0, len(s.matchers))
	for t := range s.matchers {
		types = append(types, t)
	}
	s.mu.RUnlock()

	for _, roomType := range types {
		s.mu.RLock()
		matcher := s.matchers[roomType]
		config := s.configs[roomType]
		s.mu.RUnlock()

		// 持续匹配直到无法匹配
		for {
			players := matcher.Match()
			if players == nil {
				break
			}

			roomID, err := s.manager.CreateRoom(config, nil)
			if err != nil {
				continue
			}

			for _, p := range players {
				s.manager.JoinRoom(roomID, p)
			}
		}
	}
}
