package room

import "math"

// --- ELO 评分系统 ---

// EloResult 对局结果（对 playerA 而言）
type EloResult int

const (
	EloWin  EloResult = 1  // 赢
	EloDraw EloResult = 0  // 平
	EloLoss EloResult = -1 // 输
)

// EloConfig ELO 计算配置
type EloConfig struct {
	// KFactor K 值，控制评分变化幅度（新手/竞技分别用 32 / 16 / 10）
	KFactor float64
	// MinRating 最低评分（防止无限下降）
	MinRating float64
	// Scale 等级分分母（通常为 400）
	Scale float64
}

// DefaultEloConfig 默认配置（32K，最低 0，分母 400）
func DefaultEloConfig() EloConfig {
	return EloConfig{KFactor: 32, MinRating: 0, Scale: 400}
}

// Expected 玩家 A 对 B 的期望胜率（ELO 标准公式）
func (c EloConfig) Expected(ratingA, ratingB float64) float64 {
	if c.Scale <= 0 {
		c.Scale = 400
	}
	return 1.0 / (1.0 + math.Pow(10, (ratingB-ratingA)/c.Scale))
}

// UpdateRating 根据结果更新双方评分，返回 (newA, newB)
func (c EloConfig) UpdateRating(ratingA, ratingB float64, result EloResult) (float64, float64) {
	if c.KFactor <= 0 {
		c.KFactor = 32
	}
	if c.Scale <= 0 {
		c.Scale = 400
	}
	expA := c.Expected(ratingA, ratingB)
	var scoreA float64
	switch result {
	case EloWin:
		scoreA = 1
	case EloDraw:
		scoreA = 0.5
	case EloLoss:
		scoreA = 0
	}
	deltaA := c.KFactor * (scoreA - expA)
	newA := ratingA + deltaA
	newB := ratingB - deltaA
	if newA < c.MinRating {
		newA = c.MinRating
	}
	if newB < c.MinRating {
		newB = c.MinRating
	}
	return newA, newB
}

// UpdateTeam 多人对战：A 队（winners）与 B 队（losers）结算
// 算法：取两队平均 ELO 做对比，按 K 值拆分到每个玩家身上
// 返回以 PlayerID 为键的评分增量 map。
func (c EloConfig) UpdateTeam(winners, losers []PlayerInfo) map[string]float64 {
	if len(winners) == 0 || len(losers) == 0 {
		return nil
	}
	avgW := 0.0
	for _, p := range winners {
		avgW += p.Rating
	}
	avgW /= float64(len(winners))
	avgL := 0.0
	for _, p := range losers {
		avgL += p.Rating
	}
	avgL /= float64(len(losers))

	expW := c.Expected(avgW, avgL)
	k := c.KFactor
	if k <= 0 {
		k = 32
	}
	deltaW := k * (1 - expW)
	deltaL := k * (0 - (1 - expW))

	out := make(map[string]float64, len(winners)+len(losers))
	for _, p := range winners {
		out[p.PlayerID] = deltaW
	}
	for _, p := range losers {
		out[p.PlayerID] = deltaL
	}
	return out
}

// --- MMR 隐分系统 ---
// 玩家看到的是显式段位（基于 Rating），而内部匹配使用 MMR 隐分，
// 新号和挂机号的 MMR 差异让匹配更真实。

// MMRStore MMR 存储（由上层提供持久化）
type MMRStore interface {
	GetMMR(playerID string) float64
	SetMMR(playerID string, mmr float64)
}

// InMemoryMMRStore 简单内存实现（默认值 = initial）
type InMemoryMMRStore struct {
	data    map[string]float64
	initial float64
}

// NewInMemoryMMRStore 创建内存 MMR 库
func NewInMemoryMMRStore(initial float64) *InMemoryMMRStore {
	return &InMemoryMMRStore{data: make(map[string]float64), initial: initial}
}

// GetMMR 获取 MMR；玩家不存在则返回初始值
func (s *InMemoryMMRStore) GetMMR(playerID string) float64 {
	if v, ok := s.data[playerID]; ok {
		return v
	}
	return s.initial
}

// SetMMR 更新 MMR
func (s *InMemoryMMRStore) SetMMR(playerID string, mmr float64) {
	s.data[playerID] = mmr
}

// EffectiveRating 计算玩家实际用于匹配的分数。
// - 如果 MMRStore != nil，优先使用 MMR 隐分；
// - 否则退回 PlayerInfo.Rating。
// - 可通过 metadataKey "mmr_weight" 在 [0,1] 之间调节 MMR 与 Rating 的融合。
func EffectiveRating(p PlayerInfo, store MMRStore) float64 {
	if store == nil {
		return p.Rating
	}
	mmr := store.GetMMR(p.PlayerID)
	weight := 1.0
	if p.Metadata != nil {
		if v, ok := p.Metadata["mmr_weight"]; ok {
			if f, ok := v.(float64); ok && f >= 0 && f <= 1 {
				weight = f
			}
		}
	}
	return weight*mmr + (1-weight)*p.Rating
}
