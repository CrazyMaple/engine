package leaderboard

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// --- 排行榜核心数据结构：跳表 ---

const maxLevel = 32

// Entry 排行榜条目
type Entry struct {
	PlayerID string  `json:"player_id"`
	Score    float64 `json:"score"`
	Extra    string  `json:"extra,omitempty"` // 附加数据（如玩家名称）
	UpdateAt int64   `json:"update_at"`
}

// skipNode 跳表节点
type skipNode struct {
	entry Entry
	next  []*skipNode
	span  []int // span[i] 表示第 i 层到 next[i] 跨越的元素数
}

// SkipList 有序跳表（按 Score 降序），支持 O(log N) 插入/查询/排名
type SkipList struct {
	head  *skipNode
	level int
	size  int
	mu    sync.RWMutex
	index map[string]*skipNode // playerID -> node 快速查找
}

// NewSkipList 创建跳表
func NewSkipList() *SkipList {
	head := &skipNode{
		next: make([]*skipNode, maxLevel),
		span: make([]int, maxLevel),
	}
	return &SkipList{
		head:  head,
		level: 1,
		index: make(map[string]*skipNode),
	}
}

func randomLevel() int {
	lvl := 1
	for lvl < maxLevel && rand.Float64() < 0.25 {
		lvl++
	}
	return lvl
}

// Upsert 插入或更新分数，返回新排名（1-based）
func (sl *SkipList) Upsert(playerID string, score float64, extra string) int {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// 如果已存在，先删除旧记录
	if _, ok := sl.index[playerID]; ok {
		sl.removeLocked(playerID)
	}

	now := time.Now().Unix()
	entry := Entry{
		PlayerID: playerID,
		Score:    score,
		Extra:    extra,
		UpdateAt: now,
	}

	lvl := randomLevel()
	oldLevel := sl.level
	if lvl > sl.level {
		// 新增的层级 head 的 span 应指向当前全部元素+1（跨过所有已有节点）
		for i := oldLevel; i < lvl; i++ {
			sl.head.span[i] = sl.size + 1
		}
		sl.level = lvl
	}

	newNode := &skipNode{
		entry: entry,
		next:  make([]*skipNode, lvl),
		span:  make([]int, lvl),
	}

	// 降序插入：score 大的在前面
	// rank[i] 记录在第 i 层 update[i] 节点前累积的排名偏移
	update := make([]*skipNode, maxLevel)
	rank := make([]int, maxLevel)
	curr := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		if i == sl.level-1 {
			rank[i] = 0
		} else {
			rank[i] = rank[i+1]
		}
		for curr.next[i] != nil && (curr.next[i].entry.Score > score ||
			(curr.next[i].entry.Score == score && curr.next[i].entry.UpdateAt < now)) {
			rank[i] += curr.span[i]
			curr = curr.next[i]
		}
		update[i] = curr
	}

	// 插入新节点并更新 span
	for i := 0; i < lvl; i++ {
		newNode.next[i] = update[i].next[i]
		update[i].next[i] = newNode

		// newNode.span[i] = update[i] 原来的 span - (新节点在 level 0 的位置差)
		newNode.span[i] = update[i].span[i] - (rank[0] - rank[i])
		update[i].span[i] = rank[0] - rank[i] + 1
	}

	// 更高层的 update 节点 span +1（因为插入了一个新元素）
	for i := lvl; i < sl.level; i++ {
		update[i].span[i]++
	}

	sl.index[playerID] = newNode
	sl.size++

	return sl.getRankLocked(playerID)
}

// Remove 删除条目
func (sl *SkipList) Remove(playerID string) bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.removeLocked(playerID)
}

func (sl *SkipList) removeLocked(playerID string) bool {
	node, ok := sl.index[playerID]
	if !ok {
		return false
	}

	update := make([]*skipNode, maxLevel)
	curr := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for curr.next[i] != nil && curr.next[i] != node {
			if curr.next[i].entry.Score > node.entry.Score ||
				(curr.next[i].entry.Score == node.entry.Score && curr.next[i].entry.UpdateAt < node.entry.UpdateAt) {
				curr = curr.next[i]
			} else {
				break
			}
		}
		update[i] = curr
	}

	for i := 0; i < len(node.next); i++ {
		if update[i].next[i] == node {
			update[i].next[i] = node.next[i]
			update[i].span[i] += node.span[i] - 1
		}
	}

	// 更高层的 update 节点 span -1
	for i := len(node.next); i < sl.level; i++ {
		update[i].span[i]--
	}

	// 缩减空层级
	for sl.level > 1 && sl.head.next[sl.level-1] == nil {
		sl.level--
	}

	delete(sl.index, playerID)
	sl.size--
	return true
}

// GetRank 获取排名（1-based），不存在返回 -1
func (sl *SkipList) GetRank(playerID string) int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.getRankLocked(playerID)
}

func (sl *SkipList) getRankLocked(playerID string) int {
	node, ok := sl.index[playerID]
	if !ok {
		return -1
	}

	// O(log N)：从高层开始累加 span，利用跳表层级加速
	rank := 0
	curr := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for curr.next[i] != nil && curr.next[i] != node {
			if curr.next[i].entry.Score > node.entry.Score ||
				(curr.next[i].entry.Score == node.entry.Score && curr.next[i].entry.UpdateAt < node.entry.UpdateAt) {
				rank += curr.span[i]
				curr = curr.next[i]
			} else {
				break
			}
		}
		if curr.next[i] == node {
			rank += curr.span[i]
			return rank
		}
	}
	return -1
}

// GetEntry 获取指定玩家的条目
func (sl *SkipList) GetEntry(playerID string) (Entry, bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	node, ok := sl.index[playerID]
	if !ok {
		return Entry{}, false
	}
	return node.entry, true
}

// TopN 获取前 N 名
func (sl *SkipList) TopN(n int) []RankedEntry {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	if n <= 0 || sl.size == 0 {
		return nil
	}
	if n > sl.size {
		n = sl.size
	}

	result := make([]RankedEntry, 0, n)
	curr := sl.head.next[0]
	rank := 1
	for curr != nil && rank <= n {
		result = append(result, RankedEntry{
			Rank:  rank,
			Entry: curr.entry,
		})
		rank++
		curr = curr.next[0]
	}
	return result
}

// AroundMe 获取某玩家排名前后 count 名
func (sl *SkipList) AroundMe(playerID string, count int) []RankedEntry {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	myRank := sl.getRankLocked(playerID)
	if myRank < 0 {
		return nil
	}

	startRank := myRank - count
	if startRank < 1 {
		startRank = 1
	}
	endRank := myRank + count

	result := make([]RankedEntry, 0, endRank-startRank+1)
	curr := sl.head.next[0]
	rank := 1
	for curr != nil && rank <= endRank {
		if rank >= startRank {
			result = append(result, RankedEntry{
				Rank:  rank,
				Entry: curr.entry,
			})
		}
		rank++
		curr = curr.next[0]
	}
	return result
}

// Size 返回条目数
func (sl *SkipList) Size() int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.size
}

// All 获取全部条目（按排名序）
func (sl *SkipList) All() []RankedEntry {
	return sl.TopN(sl.Size())
}

// RankedEntry 带排名的条目
type RankedEntry struct {
	Rank  int   `json:"rank"`
	Entry Entry `json:"entry"`
}

// --- 排行榜管理器 ---

// ResetPolicy 重置策略
type ResetPolicy int

const (
	ResetNone   ResetPolicy = iota // 不自动重置
	ResetDaily                     // 每日重置
	ResetWeekly                    // 每周重置
	ResetSeason                    // 赛季重置
)

// BoardConfig 排行榜配置
type BoardConfig struct {
	Name        string      // 排行榜名称/ID
	MaxSize     int         // 最大条目数（0=不限制）
	ResetPolicy ResetPolicy // 重置策略
}

// Board 单个排行榜实例
type Board struct {
	config    BoardConfig
	data      *SkipList
	lastReset time.Time
}

// NewBoard 创建排行榜
func NewBoard(cfg BoardConfig) *Board {
	return &Board{
		config:    cfg,
		data:      NewSkipList(),
		lastReset: time.Now(),
	}
}

// UpdateScore 更新分数
func (b *Board) UpdateScore(playerID string, score float64, extra string) int {
	rank := b.data.Upsert(playerID, score, extra)

	// 如果超过 MaxSize，裁剪末尾
	if b.config.MaxSize > 0 && b.data.Size() > b.config.MaxSize {
		entries := b.data.TopN(b.data.Size())
		for i := b.config.MaxSize; i < len(entries); i++ {
			b.data.Remove(entries[i].Entry.PlayerID)
		}
	}

	return rank
}

// GetRank 获取排名
func (b *Board) GetRank(playerID string) (int, *Entry) {
	rank := b.data.GetRank(playerID)
	if rank < 0 {
		return -1, nil
	}
	entry, _ := b.data.GetEntry(playerID)
	return rank, &entry
}

// GetTopN 获取前 N 名
func (b *Board) GetTopN(n int) []RankedEntry {
	return b.data.TopN(n)
}

// GetAroundMe 获取某玩家前后 count 名
func (b *Board) GetAroundMe(playerID string, count int) []RankedEntry {
	return b.data.AroundMe(playerID, count)
}

// Reset 重置排行榜
func (b *Board) Reset() {
	b.data = NewSkipList()
	b.lastReset = time.Now()
}

// ShouldReset 检查是否应该按策略重置
func (b *Board) ShouldReset(now time.Time) bool {
	switch b.config.ResetPolicy {
	case ResetDaily:
		return now.YearDay() != b.lastReset.YearDay() || now.Year() != b.lastReset.Year()
	case ResetWeekly:
		_, w1 := b.lastReset.ISOWeek()
		_, w2 := now.ISOWeek()
		return w1 != w2 || now.Year() != b.lastReset.Year()
	default:
		return false
	}
}

// Snapshot 生成快照用于持久化
func (b *Board) Snapshot() *BoardSnapshot {
	return &BoardSnapshot{
		Name:      b.config.Name,
		Entries:   b.data.All(),
		LastReset: b.lastReset.Unix(),
	}
}

// RestoreFromSnapshot 从快照恢复
func (b *Board) RestoreFromSnapshot(snap *BoardSnapshot) {
	b.data = NewSkipList()
	for _, re := range snap.Entries {
		b.data.Upsert(re.Entry.PlayerID, re.Entry.Score, re.Entry.Extra)
	}
	b.lastReset = time.Unix(snap.LastReset, 0)
}

// Size 返回条目数
func (b *Board) Size() int {
	return b.data.Size()
}

// BoardSnapshot 排行榜快照
type BoardSnapshot struct {
	Name      string        `json:"name"`
	Entries   []RankedEntry `json:"entries"`
	LastReset int64         `json:"last_reset"`
}

// --- 消息定义 ---

// UpdateScoreRequest 更新分数请求
type UpdateScoreRequest struct {
	Board    string  `json:"board"`
	PlayerID string  `json:"player_id"`
	Score    float64 `json:"score"`
	Extra    string  `json:"extra,omitempty"`
}

// UpdateScoreResponse 更新分数响应
type UpdateScoreResponse struct {
	Rank int `json:"rank"`
}

// GetRankRequest 获取排名请求
type GetRankRequest struct {
	Board    string `json:"board"`
	PlayerID string `json:"player_id"`
}

// GetRankResponse 获取排名响应
type GetRankResponse struct {
	Rank  int    `json:"rank"`
	Entry *Entry `json:"entry,omitempty"`
}

// GetTopNRequest 获取前 N 名请求
type GetTopNRequest struct {
	Board string `json:"board"`
	N     int    `json:"n"`
}

// GetTopNResponse 获取前 N 名响应
type GetTopNResponse struct {
	Entries []RankedEntry `json:"entries"`
}

// GetAroundMeRequest 获取周围排名请求
type GetAroundMeRequest struct {
	Board    string `json:"board"`
	PlayerID string `json:"player_id"`
	Count    int    `json:"count"`
}

// GetAroundMeResponse 获取周围排名响应
type GetAroundMeResponse struct {
	Entries []RankedEntry `json:"entries"`
}

// ResetBoardRequest 重置排行榜请求
type ResetBoardRequest struct {
	Board string `json:"board"`
}

// --- LeaderboardActor ---

// LeaderboardActor 排行榜 Actor，管理多个排行榜
// 可作为 Cluster Singleton 运行，保证全局唯一
type LeaderboardActor struct {
	boards       map[string]*Board
	snapshotFn   func(snap map[string]*BoardSnapshot) // 快照持久化回调
	snapshotTick int                                   // 每 N 次更新后触发快照
	updateCount  int
}

// LeaderboardConfig 排行榜 Actor 配置
type LeaderboardConfig struct {
	// Boards 初始排行榜配置
	Boards []BoardConfig
	// SnapshotFn 快照持久化回调（可选）
	SnapshotFn func(snap map[string]*BoardSnapshot)
	// SnapshotInterval 每 N 次更新后自动快照（默认 100）
	SnapshotInterval int
}

// NewLeaderboardActor 创建排行榜 Actor
func NewLeaderboardActor(cfg LeaderboardConfig) *LeaderboardActor {
	if cfg.SnapshotInterval <= 0 {
		cfg.SnapshotInterval = 100
	}

	la := &LeaderboardActor{
		boards:       make(map[string]*Board),
		snapshotFn:   cfg.SnapshotFn,
		snapshotTick: cfg.SnapshotInterval,
	}

	for _, bc := range cfg.Boards {
		la.boards[bc.Name] = NewBoard(bc)
	}

	return la
}

// GetOrCreateBoard 获取或创建排行榜
func (la *LeaderboardActor) GetOrCreateBoard(name string) *Board {
	b, ok := la.boards[name]
	if !ok {
		b = NewBoard(BoardConfig{Name: name})
		la.boards[name] = b
	}
	return b
}

// ProcessMessage 处理消息（供 Actor.Receive 使用）
func (la *LeaderboardActor) ProcessMessage(msg interface{}) interface{} {
	switch m := msg.(type) {
	case *UpdateScoreRequest:
		board := la.GetOrCreateBoard(m.Board)
		// 按策略检查是否需要重置
		if board.ShouldReset(time.Now()) {
			board.Reset()
		}
		rank := board.UpdateScore(m.PlayerID, m.Score, m.Extra)
		la.updateCount++
		la.maybeSnapshot()
		return &UpdateScoreResponse{Rank: rank}

	case *GetRankRequest:
		board := la.GetOrCreateBoard(m.Board)
		rank, entry := board.GetRank(m.PlayerID)
		return &GetRankResponse{Rank: rank, Entry: entry}

	case *GetTopNRequest:
		board := la.GetOrCreateBoard(m.Board)
		entries := board.GetTopN(m.N)
		return &GetTopNResponse{Entries: entries}

	case *GetAroundMeRequest:
		board := la.GetOrCreateBoard(m.Board)
		entries := board.GetAroundMe(m.PlayerID, m.Count)
		return &GetAroundMeResponse{Entries: entries}

	case *ResetBoardRequest:
		if board, ok := la.boards[m.Board]; ok {
			board.Reset()
		}
		return &struct{}{}

	default:
		return fmt.Errorf("unknown message: %T", msg)
	}
}

func (la *LeaderboardActor) maybeSnapshot() {
	if la.snapshotFn == nil {
		return
	}
	if la.updateCount%la.snapshotTick != 0 {
		return
	}
	snap := make(map[string]*BoardSnapshot, len(la.boards))
	for name, board := range la.boards {
		snap[name] = board.Snapshot()
	}
	la.snapshotFn(snap)
}
