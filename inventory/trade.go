package inventory

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"engine/saga"
)

// TradeStatus 交易状态
type TradeStatus int

const (
	TradeStatusPending   TradeStatus = iota // 已创建（双方可修改物品列表）
	TradeStatusConfirmed                    // 双方已确认
	TradeStatusRunning                      // Saga 执行中
	TradeStatusCompleted                    // 交易成功
	TradeStatusCancelled                    // 取消
	TradeStatusFailed                       // 执行/补偿失败
)

func (s TradeStatus) String() string {
	switch s {
	case TradeStatusPending:
		return "pending"
	case TradeStatusConfirmed:
		return "confirmed"
	case TradeStatusRunning:
		return "running"
	case TradeStatusCompleted:
		return "completed"
	case TradeStatusCancelled:
		return "cancelled"
	case TradeStatusFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// TradeItem 交易中要付出的物品
type TradeItem struct {
	TemplateID int
	Quantity   int
}

// TradeParty 交易一方
type TradeParty struct {
	PlayerID  string
	Inv       *Inventory
	Offer     []TradeItem
	Confirmed bool
}

// TradeSession 交易会话
// 语义：A 付出 OfferA 换取 B 付出的 OfferB，反之亦然；要么都成功，要么都回滚
type TradeSession struct {
	ID        string
	A         *TradeParty
	B         *TradeParty
	Status    TradeStatus
	CreatedAt time.Time
	Timeout   time.Duration
	Error     error

	mu sync.Mutex

	// removedFromA/removedFromB 执行时记录已移除的道具快照（用于补偿）
	removedFromA []TradeItem
	removedFromB []TradeItem
	// addedToA/addedToB 执行时记录已添加到目标背包的道具模板数量（补偿时从对方背包扣除）
	addedToA []TradeItem
	addedToB []TradeItem
}

// newTradeSession 创建会话（内部构造）
func newTradeSession(id string, a, b *TradeParty, timeout time.Duration) *TradeSession {
	return &TradeSession{
		ID:        id,
		A:         a,
		B:         b,
		Status:    TradeStatusPending,
		CreatedAt: time.Now(),
		Timeout:   timeout,
	}
}

// SetOffer 更新某一方的物品列表（需在 Pending 状态）
func (ts *TradeSession) SetOffer(playerID string, items []TradeItem) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.Status != TradeStatusPending {
		return fmt.Errorf("cannot modify offer in status %s", ts.Status)
	}
	switch playerID {
	case ts.A.PlayerID:
		ts.A.Offer = copyOffer(items)
		ts.A.Confirmed = false
		ts.B.Confirmed = false
	case ts.B.PlayerID:
		ts.B.Offer = copyOffer(items)
		ts.A.Confirmed = false
		ts.B.Confirmed = false
	default:
		return fmt.Errorf("player %s not in trade", playerID)
	}
	return nil
}

// Confirm 一方确认，当双方都确认时转入 Confirmed 状态
func (ts *TradeSession) Confirm(playerID string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.Status != TradeStatusPending {
		return fmt.Errorf("cannot confirm in status %s", ts.Status)
	}
	switch playerID {
	case ts.A.PlayerID:
		ts.A.Confirmed = true
	case ts.B.PlayerID:
		ts.B.Confirmed = true
	default:
		return fmt.Errorf("player %s not in trade", playerID)
	}
	if ts.A.Confirmed && ts.B.Confirmed {
		ts.Status = TradeStatusConfirmed
	}
	return nil
}

// Cancel 取消交易（仅当未进入 Running/Completed 时可取消）
func (ts *TradeSession) Cancel(reason string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.Status == TradeStatusRunning || ts.Status == TradeStatusCompleted {
		return fmt.Errorf("cannot cancel in status %s", ts.Status)
	}
	ts.Status = TradeStatusCancelled
	if reason != "" {
		ts.Error = fmt.Errorf("%s", reason)
	}
	return nil
}

// copyOffer 深拷贝避免调用方后续修改影响会话
func copyOffer(items []TradeItem) []TradeItem {
	out := make([]TradeItem, len(items))
	copy(out, items)
	return out
}

// TradeManager 交易管理器
// - 在两侧背包上按 playerID 排序加锁避免死锁
// - 用 saga.Coordinator 编排原子交易 + 补偿
// - 支持超时自动取消：后台 goroutine 扫描过期会话
type TradeManager struct {
	coord *saga.Coordinator
	mu    sync.Mutex
	// 每个玩家的背包访问锁
	locks map[string]*sync.Mutex
	// 活跃交易
	sessions map[string]*TradeSession
	// 交易 ID 生成
	seq uint64
	// 默认超时
	defaultTimeout time.Duration
}

// NewTradeManager 创建交易管理器
func NewTradeManager(coord *saga.Coordinator, defaultTimeout time.Duration) *TradeManager {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	return &TradeManager{
		coord:          coord,
		locks:          make(map[string]*sync.Mutex),
		sessions:       make(map[string]*TradeSession),
		defaultTimeout: defaultTimeout,
	}
}

// BeginTrade 创建双方交易会话
func (tm *TradeManager) BeginTrade(playerA string, invA *Inventory, playerB string, invB *Inventory) (*TradeSession, error) {
	if playerA == "" || playerB == "" || playerA == playerB {
		return nil, fmt.Errorf("invalid trade parties")
	}
	if invA == nil || invB == nil {
		return nil, fmt.Errorf("both inventories required")
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	atomic.AddUint64(&tm.seq, 1)
	id := fmt.Sprintf("trade_%d_%d", time.Now().UnixNano(), tm.seq)
	session := newTradeSession(id,
		&TradeParty{PlayerID: playerA, Inv: invA},
		&TradeParty{PlayerID: playerB, Inv: invB},
		tm.defaultTimeout,
	)
	tm.sessions[id] = session
	return session, nil
}

// GetTrade 按 ID 查找
func (tm *TradeManager) GetTrade(id string) (*TradeSession, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	s, ok := tm.sessions[id]
	return s, ok
}

// getLock 获取玩家级锁（按需创建）
func (tm *TradeManager) getLock(playerID string) *sync.Mutex {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if l, ok := tm.locks[playerID]; ok {
		return l
	}
	l := &sync.Mutex{}
	tm.locks[playerID] = l
	return l
}

// Execute 执行已双方确认的交易：顺序扣减双方道具 → 互换发放；失败时反向补偿
func (tm *TradeManager) Execute(session *TradeSession) error {
	session.mu.Lock()
	if session.Status != TradeStatusConfirmed {
		err := fmt.Errorf("trade %s not confirmed (status=%s)", session.ID, session.Status)
		session.mu.Unlock()
		return err
	}
	session.Status = TradeStatusRunning
	session.mu.Unlock()

	// 按 playerID 字典序加锁避免死锁
	ids := []string{session.A.PlayerID, session.B.PlayerID}
	sort.Strings(ids)
	l1 := tm.getLock(ids[0])
	l2 := tm.getLock(ids[1])
	l1.Lock()
	defer l1.Unlock()
	l2.Lock()
	defer l2.Unlock()

	// 构建 Saga
	sagaDef := saga.NewSaga("trade_" + session.ID).
		WithTimeout(session.Timeout).
		Step("validate_both",
			func(ctx *saga.SagaContext) error { return validateOffers(session) },
			nil, // 验证步骤无副作用，不需要补偿
		).
		Step("debit_A",
			func(ctx *saga.SagaContext) error { return debit(session.A, &session.removedFromA) },
			func(ctx *saga.SagaContext) error { return refund(session.A, session.removedFromA) },
		).
		Step("debit_B",
			func(ctx *saga.SagaContext) error { return debit(session.B, &session.removedFromB) },
			func(ctx *saga.SagaContext) error { return refund(session.B, session.removedFromB) },
		).
		Step("credit_A",
			func(ctx *saga.SagaContext) error { return credit(session.A.Inv, session.B.Offer, &session.addedToA) },
			func(ctx *saga.SagaContext) error { return reverseCredit(session.A.Inv, session.addedToA) },
		).
		Step("credit_B",
			func(ctx *saga.SagaContext) error { return credit(session.B.Inv, session.A.Offer, &session.addedToB) },
			func(ctx *saga.SagaContext) error { return reverseCredit(session.B.Inv, session.addedToB) },
		).
		Build()

	sagaCtx := &saga.SagaContext{SagaID: session.ID}
	exec, err := tm.coord.Execute(sagaCtx, sagaDef)

	session.mu.Lock()
	defer session.mu.Unlock()
	if err != nil {
		if exec != nil && exec.Status == saga.SagaStatusCompensated {
			session.Status = TradeStatusCancelled
		} else {
			session.Status = TradeStatusFailed
		}
		session.Error = err
		return err
	}
	session.Status = TradeStatusCompleted
	return nil
}

// CleanupExpired 扫描并取消超时未执行的交易
func (tm *TradeManager) CleanupExpired(now time.Time) int {
	tm.mu.Lock()
	sessions := make([]*TradeSession, 0, len(tm.sessions))
	for _, s := range tm.sessions {
		sessions = append(sessions, s)
	}
	tm.mu.Unlock()

	cancelled := 0
	for _, s := range sessions {
		s.mu.Lock()
		expired := s.Timeout > 0 && now.Sub(s.CreatedAt) > s.Timeout &&
			(s.Status == TradeStatusPending || s.Status == TradeStatusConfirmed)
		s.mu.Unlock()
		if !expired {
			continue
		}
		if err := s.Cancel("timeout"); err == nil {
			cancelled++
		}
	}
	return cancelled
}

// --- Saga Step 实现 ---

// validateOffers 校验双方出价在各自背包中存在且数量充足
func validateOffers(session *TradeSession) error {
	if len(session.A.Offer) == 0 && len(session.B.Offer) == 0 {
		return fmt.Errorf("empty offers on both sides")
	}
	if err := validateParty(session.A); err != nil {
		return fmt.Errorf("player %s: %w", session.A.PlayerID, err)
	}
	if err := validateParty(session.B); err != nil {
		return fmt.Errorf("player %s: %w", session.B.PlayerID, err)
	}
	return nil
}

func validateParty(p *TradeParty) error {
	counts := aggregate(p.Offer)
	for tmpl, qty := range counts {
		have := p.Inv.CountByTemplate(tmpl)
		if have < qty {
			return fmt.Errorf("item %d insufficient: need %d, have %d", tmpl, qty, have)
		}
	}
	return nil
}

// debit 从背包中扣减 offer 中的道具，记录已扣减列表以便补偿
func debit(p *TradeParty, record *[]TradeItem) error {
	*record = (*record)[:0]
	counts := aggregate(p.Offer)
	for tmpl, qty := range counts {
		if err := p.Inv.RemoveByTemplate(tmpl, qty); err != nil {
			return fmt.Errorf("debit template %d: %w", tmpl, err)
		}
		*record = append(*record, TradeItem{TemplateID: tmpl, Quantity: qty})
	}
	return nil
}

// refund 退回已扣减的道具
func refund(p *TradeParty, removed []TradeItem) error {
	for _, it := range removed {
		if _, err := p.Inv.AddItem(it.TemplateID, it.Quantity); err != nil {
			return fmt.Errorf("refund template %d x%d: %w", it.TemplateID, it.Quantity, err)
		}
	}
	return nil
}

// credit 把对方 offer 添加到本方背包，失败则当前已加的部分会被 reverseCredit 回滚
func credit(inv *Inventory, offer []TradeItem, added *[]TradeItem) error {
	*added = (*added)[:0]
	counts := aggregate(offer)
	for tmpl, qty := range counts {
		if _, err := inv.AddItem(tmpl, qty); err != nil {
			return fmt.Errorf("credit template %d x%d: %w", tmpl, qty, err)
		}
		*added = append(*added, TradeItem{TemplateID: tmpl, Quantity: qty})
	}
	return nil
}

// reverseCredit 从本方背包扣除已经 credit 过的道具
func reverseCredit(inv *Inventory, added []TradeItem) error {
	for _, it := range added {
		if err := inv.RemoveByTemplate(it.TemplateID, it.Quantity); err != nil {
			return fmt.Errorf("reverse credit template %d: %w", it.TemplateID, err)
		}
	}
	return nil
}

// aggregate 按 templateID 聚合数量
func aggregate(items []TradeItem) map[int]int {
	out := make(map[int]int, len(items))
	for _, it := range items {
		if it.Quantity <= 0 {
			continue
		}
		out[it.TemplateID] += it.Quantity
	}
	return out
}
