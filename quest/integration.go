package quest

import (
	"fmt"
	"sync"
	"time"

	"engine/actor"
)

// --- 跨模块集成契约 ---
//
// 不直接 import mail/inventory/leaderboard，通过适配器接口让业务层注入，保持 quest 包独立。
// 集成层职责：
//   1. 任务完成领奖后：
//      - "item" 奖励 → ItemGranter.Grant（写入背包），背包满则回落到邮件附件
//      - "mail" 奖励 → Mailer.SendReward（邮件正文）
//      - 兜底邮件（所有类型奖励派发失败时兜底留档）
//   2. 成就达成：
//      - 积分 → LeaderboardUpdater.UpdateAchievementPoints
//      - 奖励 → ItemGranter / Mailer 同上
//
// 联动通过 actor.EventStream 解耦：QuestCompleted / AchievementUnlocked 事件发布，
// Integrator 作为订阅者接管后续分发。

// RewardItem 解析后的道具奖励
type RewardItem struct {
	ItemID string
	Count  int
}

// ItemGranter 道具发放接口（由 inventory 适配）
// 返回 overflow 时，表示未能入背包的部分，调用方可回落到邮件附件
type ItemGranter interface {
	Grant(playerID string, items []RewardItem) (overflow []RewardItem, err error)
}

// Mailer 邮件接口（由 mail 适配）
// attachments 可能包含道具/货币等，由业务层自行解释
type Mailer interface {
	SendReward(playerID, subject, content string, attachments []RewardItem) error
}

// LeaderboardUpdater 排行榜接口（由 leaderboard 适配）
type LeaderboardUpdater interface {
	UpdateAchievementPoints(playerID string, points int) (rank int, err error)
}

// ExpGranter 经验/金币奖励接口（可选）
type ExpGranter interface {
	GrantExp(playerID string, amount int) error
	GrantGold(playerID string, amount int) error
}

// --- 事件定义（通过 EventStream 投递）---

// QuestCompletedEvent 任务完成事件
type QuestCompletedEvent struct {
	PlayerID  string
	QuestID   string
	Rewards   []RewardDef
	Timestamp time.Time
}

// AchievementUnlockedEvent 成就达成事件
type AchievementUnlockedEvent struct {
	PlayerID      string
	AchievementID string
	Points        int
	Rewards       []RewardDef
	Timestamp     time.Time
}

// --- Integrator 实现 ---

// Integrator 任务/成就跨模块联动器
type Integrator struct {
	mu sync.Mutex

	bus         *actor.EventStream
	sub         *actor.Subscription
	items       ItemGranter
	mailer      Mailer
	leaderboard LeaderboardUpdater
	xp          ExpGranter

	// LeaderboardName 排行榜名称（由业务层决定，通常 "achievement_points"）
	LeaderboardName string

	// LastErrors 最近的分发错误（用于诊断，按玩家 ID 保留最新一条）
	lastErrors map[string]error
}

// IntegratorConfig 构造配置
type IntegratorConfig struct {
	Bus             *actor.EventStream
	Items           ItemGranter
	Mailer          Mailer
	Leaderboard     LeaderboardUpdater
	Exp             ExpGranter
	LeaderboardName string
}

// NewIntegrator 创建联动器
func NewIntegrator(cfg IntegratorConfig) *Integrator {
	return &Integrator{
		bus:             cfg.Bus,
		items:           cfg.Items,
		mailer:          cfg.Mailer,
		leaderboard:     cfg.Leaderboard,
		xp:              cfg.Exp,
		LeaderboardName: cfg.LeaderboardName,
		lastErrors:      make(map[string]error),
	}
}

// Start 订阅 EventStream，开始接管分发
func (it *Integrator) Start() {
	if it.bus == nil {
		return
	}
	it.sub = it.bus.Subscribe(func(e interface{}) {
		switch ev := e.(type) {
		case QuestCompletedEvent:
			it.OnQuestCompleted(ev)
		case AchievementUnlockedEvent:
			it.OnAchievementUnlocked(ev)
		}
	})
}

// Stop 取消订阅
func (it *Integrator) Stop() {
	if it.sub != nil {
		it.sub.Unsubscribe()
		it.sub = nil
	}
}

// PublishQuestCompleted 业务层入口：任务完成后由 QuestTracker.ClaimRewards 触发
func (it *Integrator) PublishQuestCompleted(playerID, questID string, rewards []RewardDef) {
	ev := QuestCompletedEvent{
		PlayerID: playerID, QuestID: questID, Rewards: rewards, Timestamp: time.Now(),
	}
	if it.bus != nil {
		it.bus.Publish(ev)
	} else {
		it.OnQuestCompleted(ev)
	}
}

// PublishAchievementUnlocked 业务层入口：成就达成后由 AchievementTracker 回调触发
func (it *Integrator) PublishAchievementUnlocked(playerID, achID string, points int, rewards []RewardDef) {
	ev := AchievementUnlockedEvent{
		PlayerID: playerID, AchievementID: achID, Points: points, Rewards: rewards, Timestamp: time.Now(),
	}
	if it.bus != nil {
		it.bus.Publish(ev)
	} else {
		it.OnAchievementUnlocked(ev)
	}
}

// OnQuestCompleted 实际处理任务完成分发
func (it *Integrator) OnQuestCompleted(ev QuestCompletedEvent) {
	items, exp, gold, mailOnly := splitRewards(ev.Rewards)
	subject := fmt.Sprintf("任务奖励：%s", ev.QuestID)
	content := fmt.Sprintf("完成任务 %s 获得以下奖励", ev.QuestID)
	it.dispatch(ev.PlayerID, subject, content, items, exp, gold, mailOnly)
}

// OnAchievementUnlocked 实际处理成就达成分发
func (it *Integrator) OnAchievementUnlocked(ev AchievementUnlockedEvent) {
	// 排行榜积分
	if it.leaderboard != nil && ev.Points > 0 {
		if _, err := it.leaderboard.UpdateAchievementPoints(ev.PlayerID, ev.Points); err != nil {
			it.recordErr(ev.PlayerID, fmt.Errorf("leaderboard update: %w", err))
		}
	}
	// 奖励
	items, exp, gold, mailOnly := splitRewards(ev.Rewards)
	subject := fmt.Sprintf("成就奖励：%s", ev.AchievementID)
	content := fmt.Sprintf("达成成就 %s 获得以下奖励", ev.AchievementID)
	it.dispatch(ev.PlayerID, subject, content, items, exp, gold, mailOnly)
}

// dispatch 按顺序分发：道具→背包 → 溢出+mailOnly→邮件附件 → 经验/金币
func (it *Integrator) dispatch(playerID, subject, content string, items []RewardItem, exp int, gold int, mailOnly []RewardItem) {
	mailAttachments := append([]RewardItem{}, mailOnly...)

	if len(items) > 0 {
		if it.items == nil {
			mailAttachments = append(mailAttachments, items...)
		} else {
			overflow, err := it.items.Grant(playerID, items)
			if err != nil {
				it.recordErr(playerID, fmt.Errorf("grant items: %w", err))
			}
			mailAttachments = append(mailAttachments, overflow...)
		}
	}

	if len(mailAttachments) > 0 {
		if it.mailer == nil {
			it.recordErr(playerID, fmt.Errorf("mailer not configured for overflow %d items", len(mailAttachments)))
		} else if err := it.mailer.SendReward(playerID, subject, content, mailAttachments); err != nil {
			it.recordErr(playerID, fmt.Errorf("send reward mail: %w", err))
		}
	}

	if exp > 0 && it.xp != nil {
		if err := it.xp.GrantExp(playerID, exp); err != nil {
			it.recordErr(playerID, fmt.Errorf("grant exp: %w", err))
		}
	}
	if gold > 0 && it.xp != nil {
		if err := it.xp.GrantGold(playerID, gold); err != nil {
			it.recordErr(playerID, fmt.Errorf("grant gold: %w", err))
		}
	}
}

// LastError 返回指定玩家最近一次分发错误（测试/诊断用途）
func (it *Integrator) LastError(playerID string) error {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.lastErrors[playerID]
}

func (it *Integrator) recordErr(playerID string, err error) {
	it.mu.Lock()
	defer it.mu.Unlock()
	it.lastErrors[playerID] = err
}

// splitRewards 把 RewardDef 列表拆成 items/exp/gold/mailOnly 四类
func splitRewards(rewards []RewardDef) (items []RewardItem, exp int, gold int, mailOnly []RewardItem) {
	for _, r := range rewards {
		if r.Count <= 0 {
			continue
		}
		switch r.Type {
		case "item":
			items = append(items, RewardItem{ItemID: r.ItemID, Count: r.Count})
		case "exp":
			exp += r.Count
		case "gold":
			gold += r.Count
		case "mail":
			// "mail" 类型的奖励直接走邮件（由邮件系统自行派发）
			mailOnly = append(mailOnly, RewardItem{ItemID: r.ItemID, Count: r.Count})
		}
	}
	return
}

// --- 工厂辅助：把 AchievementTracker 的回调接入 Integrator ---

// AttachAchievementTracker 绑定成就追踪器，达成时自动发布事件
func (it *Integrator) AttachAchievementTracker(tracker *AchievementTracker) {
	tracker.SetOnAchieved(func(playerID string, ach *AchievementInstance) {
		it.PublishAchievementUnlocked(playerID, ach.Def.ID, ach.Def.Points, ach.Def.Rewards)
	})
}

// ClaimQuestRewards 便利方法：领取任务奖励并通过 Integrator 分发
// 封装 tracker.ClaimRewards + PublishQuestCompleted 两步
func (it *Integrator) ClaimQuestRewards(tracker *QuestTracker, questID string) error {
	rewards, err := tracker.ClaimRewards(questID)
	if err != nil {
		return err
	}
	it.PublishQuestCompleted(tracker.playerID, questID, rewards)
	return nil
}
