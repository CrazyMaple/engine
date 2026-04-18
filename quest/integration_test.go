package quest

import (
	"errors"
	"sync"
	"testing"
	"time"

	"engine/actor"
)

// --- 测试用 mock 适配器 ---

type mockItemGranter struct {
	mu       sync.Mutex
	granted  map[string][]RewardItem // playerID → items
	overflow map[string][]RewardItem // playerID → 预设溢出
	failWith error
}

func newMockItemGranter() *mockItemGranter {
	return &mockItemGranter{
		granted:  make(map[string][]RewardItem),
		overflow: make(map[string][]RewardItem),
	}
}

func (m *mockItemGranter) Grant(playerID string, items []RewardItem) ([]RewardItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failWith != nil {
		return nil, m.failWith
	}
	over := m.overflow[playerID]
	// 真正入背包的 = items - over（简化：直接把 items 全部记为已入，over 单独返回）
	m.granted[playerID] = append(m.granted[playerID], items...)
	return over, nil
}

type mailCall struct {
	PlayerID    string
	Subject     string
	Content     string
	Attachments []RewardItem
}

type mockMailer struct {
	mu       sync.Mutex
	calls    []mailCall
	failWith error
}

func newMockMailer() *mockMailer {
	return &mockMailer{}
}

func (m *mockMailer) SendReward(playerID, subject, content string, attachments []RewardItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failWith != nil {
		return m.failWith
	}
	m.calls = append(m.calls, mailCall{
		PlayerID: playerID, Subject: subject, Content: content,
		Attachments: append([]RewardItem{}, attachments...),
	})
	return nil
}

func (m *mockMailer) lastCall() *mailCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	c := m.calls[len(m.calls)-1]
	return &c
}

type lbCall struct {
	PlayerID string
	Points   int
}

type mockLeaderboard struct {
	mu       sync.Mutex
	calls    []lbCall
	total    map[string]int
	failWith error
}

func newMockLeaderboard() *mockLeaderboard {
	return &mockLeaderboard{total: make(map[string]int)}
}

func (m *mockLeaderboard) UpdateAchievementPoints(playerID string, points int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failWith != nil {
		return 0, m.failWith
	}
	m.calls = append(m.calls, lbCall{PlayerID: playerID, Points: points})
	m.total[playerID] += points
	return 1, nil
}

type mockXP struct {
	mu       sync.Mutex
	exp      map[string]int
	gold     map[string]int
	failExp  error
	failGold error
}

func newMockXP() *mockXP {
	return &mockXP{exp: make(map[string]int), gold: make(map[string]int)}
}

func (m *mockXP) GrantExp(playerID string, amount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failExp != nil {
		return m.failExp
	}
	m.exp[playerID] += amount
	return nil
}

func (m *mockXP) GrantGold(playerID string, amount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGold != nil {
		return m.failGold
	}
	m.gold[playerID] += amount
	return nil
}

// --- 测试用例 ---

func TestIntegrator_QuestCompleted_HappyPath(t *testing.T) {
	items := newMockItemGranter()
	mailer := newMockMailer()
	xp := newMockXP()

	it := NewIntegrator(IntegratorConfig{
		Items:  items,
		Mailer: mailer,
		Exp:    xp,
	})

	rewards := []RewardDef{
		{Type: "item", ItemID: "potion", Count: 3},
		{Type: "exp", Count: 100},
		{Type: "gold", Count: 500},
	}
	it.PublishQuestCompleted("alice", "quest_001", rewards)

	if len(items.granted["alice"]) != 1 || items.granted["alice"][0].ItemID != "potion" {
		t.Errorf("item grant mismatch: %+v", items.granted)
	}
	if xp.exp["alice"] != 100 {
		t.Errorf("exp want 100, got %d", xp.exp["alice"])
	}
	if xp.gold["alice"] != 500 {
		t.Errorf("gold want 500, got %d", xp.gold["alice"])
	}
	// 没溢出，不应发邮件
	if len(mailer.calls) != 0 {
		t.Errorf("should not send mail when nothing overflows, got %d", len(mailer.calls))
	}
	if err := it.LastError("alice"); err != nil {
		t.Errorf("should have no error, got %v", err)
	}
}

// 道具溢出应回落到邮件附件
func TestIntegrator_ItemOverflowFallbackToMail(t *testing.T) {
	items := newMockItemGranter()
	// 预设：alice 的"potion"溢出 1 个
	items.overflow["alice"] = []RewardItem{{ItemID: "potion", Count: 1}}
	mailer := newMockMailer()

	it := NewIntegrator(IntegratorConfig{Items: items, Mailer: mailer})
	it.PublishQuestCompleted("alice", "q1", []RewardDef{
		{Type: "item", ItemID: "potion", Count: 5},
	})

	call := mailer.lastCall()
	if call == nil {
		t.Fatal("overflow should trigger mail")
	}
	if call.PlayerID != "alice" || len(call.Attachments) != 1 || call.Attachments[0].ItemID != "potion" {
		t.Errorf("unexpected mail: %+v", call)
	}
}

// "mail" 类型奖励直接走邮件
func TestIntegrator_MailTypeReward(t *testing.T) {
	items := newMockItemGranter()
	mailer := newMockMailer()

	it := NewIntegrator(IntegratorConfig{Items: items, Mailer: mailer})
	it.PublishQuestCompleted("alice", "q1", []RewardDef{
		{Type: "mail", ItemID: "giftbox", Count: 1},
	})

	// 道具系统不应被调用
	if len(items.granted) != 0 {
		t.Errorf("items should not be granted, got %+v", items.granted)
	}
	call := mailer.lastCall()
	if call == nil || len(call.Attachments) != 1 || call.Attachments[0].ItemID != "giftbox" {
		t.Errorf("mail reward mismatch: %+v", call)
	}
}

// 没有配置道具适配器时，item 奖励直接走邮件兜底
func TestIntegrator_NoItemGranter_FallbackMail(t *testing.T) {
	mailer := newMockMailer()

	it := NewIntegrator(IntegratorConfig{Mailer: mailer})
	it.PublishQuestCompleted("alice", "q1", []RewardDef{
		{Type: "item", ItemID: "ring", Count: 1},
	})

	call := mailer.lastCall()
	if call == nil || len(call.Attachments) != 1 || call.Attachments[0].ItemID != "ring" {
		t.Errorf("should fallback to mail when items nil: %+v", call)
	}
}

// 道具与邮件都失效时，应当记录错误
func TestIntegrator_MailerMissingRecordsError(t *testing.T) {
	items := newMockItemGranter()
	items.overflow["alice"] = []RewardItem{{ItemID: "sword", Count: 1}}

	it := NewIntegrator(IntegratorConfig{Items: items}) // Mailer 缺失
	it.PublishQuestCompleted("alice", "q1", []RewardDef{
		{Type: "item", ItemID: "sword", Count: 1},
	})
	err := it.LastError("alice")
	if err == nil {
		t.Fatal("should record error when mailer nil and overflow exists")
	}
}

// 道具发放 error 应被记录且溢出走邮件
func TestIntegrator_ItemGrantErrorRecorded(t *testing.T) {
	items := newMockItemGranter()
	items.failWith = errors.New("inventory busy")
	mailer := newMockMailer()

	it := NewIntegrator(IntegratorConfig{Items: items, Mailer: mailer})
	it.PublishQuestCompleted("alice", "q1", []RewardDef{
		{Type: "item", ItemID: "ring", Count: 2},
	})
	if it.LastError("alice") == nil {
		t.Error("should record grant error")
	}
}

// 成就达成 → 排行榜积分 + 奖励
func TestIntegrator_AchievementUnlocked(t *testing.T) {
	items := newMockItemGranter()
	mailer := newMockMailer()
	lb := newMockLeaderboard()
	xp := newMockXP()

	it := NewIntegrator(IntegratorConfig{
		Items: items, Mailer: mailer, Leaderboard: lb, Exp: xp,
		LeaderboardName: "achievement_points",
	})

	it.PublishAchievementUnlocked("alice", "first_kill", 50, []RewardDef{
		{Type: "item", ItemID: "badge", Count: 1},
		{Type: "gold", Count: 1000},
	})

	if lb.total["alice"] != 50 {
		t.Errorf("leaderboard points want 50, got %d", lb.total["alice"])
	}
	if len(items.granted["alice"]) != 1 || items.granted["alice"][0].ItemID != "badge" {
		t.Errorf("badge not granted: %+v", items.granted)
	}
	if xp.gold["alice"] != 1000 {
		t.Errorf("gold want 1000, got %d", xp.gold["alice"])
	}
}

// 通过 EventStream 订阅事件的路径
func TestIntegrator_EventStreamSubscribe(t *testing.T) {
	bus := actor.NewEventStream()
	items := newMockItemGranter()
	mailer := newMockMailer()

	it := NewIntegrator(IntegratorConfig{
		Bus: bus, Items: items, Mailer: mailer,
	})
	it.Start()
	defer it.Stop()

	it.PublishQuestCompleted("alice", "q1", []RewardDef{
		{Type: "item", ItemID: "potion", Count: 2},
	})

	// Publish 是同步的，EventStream 直接回调，无需等待
	if len(items.granted["alice"]) != 1 {
		t.Errorf("subscribe path failed: %+v", items.granted)
	}

	// Stop 之后再发事件，不应触发
	it.Stop()
	bus.Publish(QuestCompletedEvent{
		PlayerID: "bob", QuestID: "q2",
		Rewards: []RewardDef{{Type: "item", ItemID: "x", Count: 1}},
	})
	if _, ok := items.granted["bob"]; ok {
		t.Error("after stop, events must not be processed")
	}
}

// splitRewards 拆分正确
func TestIntegrator_SplitRewards(t *testing.T) {
	rewards := []RewardDef{
		{Type: "item", ItemID: "potion", Count: 3},
		{Type: "exp", Count: 100},
		{Type: "gold", Count: 50},
		{Type: "mail", ItemID: "gift", Count: 1},
		{Type: "exp", Count: 50}, // 累加
		{Type: "item", ItemID: "ring", Count: 0}, // 被过滤
		{Type: "unknown", Count: 5},              // 被过滤
	}
	items, exp, gold, mailOnly := splitRewards(rewards)
	if len(items) != 1 || items[0].ItemID != "potion" || items[0].Count != 3 {
		t.Errorf("items mismatch: %+v", items)
	}
	if exp != 150 {
		t.Errorf("exp want 150, got %d", exp)
	}
	if gold != 50 {
		t.Errorf("gold want 50, got %d", gold)
	}
	if len(mailOnly) != 1 || mailOnly[0].ItemID != "gift" {
		t.Errorf("mailOnly mismatch: %+v", mailOnly)
	}
}

// AttachAchievementTracker：成就达成回调自动走 Integrator
func TestIntegrator_AttachAchievementTracker(t *testing.T) {
	items := newMockItemGranter()
	mailer := newMockMailer()
	lb := newMockLeaderboard()

	it := NewIntegrator(IntegratorConfig{
		Items: items, Mailer: mailer, Leaderboard: lb,
	})

	defs := []*AchievementDef{
		{
			ID: "slayer", Name: "屠龙者", EventType: "kill_monster",
			TargetID: "dragon", Required: 1, Points: 100,
			Rewards: []RewardDef{{Type: "item", ItemID: "dragon_scale", Count: 1}},
		},
	}
	tracker := NewAchievementTracker("alice", defs)
	it.AttachAchievementTracker(tracker)

	tracker.HandleEvent(GameEvent{
		Type: "kill_monster", TargetID: "dragon", Count: 1,
		PlayerID: "alice",
	})

	if lb.total["alice"] != 100 {
		t.Errorf("achievement points via tracker should be 100, got %d", lb.total["alice"])
	}
	if len(items.granted["alice"]) != 1 {
		t.Errorf("reward via tracker should be granted: %+v", items.granted)
	}
}

// ClaimQuestRewards 便利方法
func TestIntegrator_ClaimQuestRewards(t *testing.T) {
	items := newMockItemGranter()
	mailer := newMockMailer()

	it := NewIntegrator(IntegratorConfig{Items: items, Mailer: mailer})

	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID: "daily_login", Name: "每日登录",
		Steps:   []StepDef{{ID: "s1", EventType: "login", Required: 1}},
		Rewards: []RewardDef{{Type: "item", ItemID: "daily_box", Count: 1}},
	})

	tracker := NewQuestTracker("alice", reg)
	if err := tracker.Accept("daily_login", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	tracker.HandleEvent(GameEvent{Type: "login", Count: 1, PlayerID: "alice"})

	if err := it.ClaimQuestRewards(tracker, "daily_login"); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if len(items.granted["alice"]) != 1 || items.granted["alice"][0].ItemID != "daily_box" {
		t.Errorf("daily_box should be granted: %+v", items.granted)
	}
}

// Start 未设置 Bus 时应无副作用
func TestIntegrator_StartWithoutBus(t *testing.T) {
	it := NewIntegrator(IntegratorConfig{})
	it.Start() // 不应 panic
	it.Stop()
}

// 同一玩家的错误会覆盖为最新一条
func TestIntegrator_LastErrorOverwrites(t *testing.T) {
	items := newMockItemGranter()
	items.failWith = errors.New("first")
	it := NewIntegrator(IntegratorConfig{Items: items})

	it.PublishQuestCompleted("alice", "q1", []RewardDef{
		{Type: "item", ItemID: "a", Count: 1},
	})
	first := it.LastError("alice")
	if first == nil {
		t.Fatal("first error missing")
	}

	items.failWith = errors.New("second")
	it.PublishQuestCompleted("alice", "q2", []RewardDef{
		{Type: "item", ItemID: "b", Count: 1},
	})
	second := it.LastError("alice")
	if second == nil || second.Error() == first.Error() {
		t.Errorf("latest error should overwrite: first=%v second=%v", first, second)
	}
}
