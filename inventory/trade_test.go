package inventory

import (
	"errors"
	"sync"
	"testing"
	"time"

	"engine/saga"
)

func setupTradeRegistry() *TemplateRegistry {
	r := NewTemplateRegistry()
	r.Register(&ItemTemplate{ID: 1, Name: "HP Potion", Type: ItemTypeConsumable, MaxStack: 99})
	r.Register(&ItemTemplate{ID: 2, Name: "MP Potion", Type: ItemTypeConsumable, MaxStack: 99})
	r.Register(&ItemTemplate{ID: 3, Name: "Gold", Type: ItemTypeCurrency, MaxStack: 999999})
	r.Register(&ItemTemplate{ID: 4, Name: "Iron Sword", Type: ItemTypeEquipment, MaxStack: 1})
	return r
}

func newTradeManager() *TradeManager {
	return NewTradeManager(saga.NewCoordinator(), 5*time.Second)
}

func TestTrade_HappyPath(t *testing.T) {
	reg := setupTradeRegistry()
	invA := NewInventory(10, reg)
	invB := NewInventory(10, reg)
	invA.AddItem(1, 10) // A 有 10 HP Potion
	invA.AddItem(3, 500) // A 有 500 Gold
	invB.AddItem(2, 20) // B 有 20 MP Potion

	mgr := newTradeManager()
	session, err := mgr.BeginTrade("alice", invA, "bob", invB)
	if err != nil {
		t.Fatal(err)
	}
	// A 提供 5 HP Potion + 100 Gold 换 B 的 10 MP Potion
	session.SetOffer("alice", []TradeItem{{TemplateID: 1, Quantity: 5}, {TemplateID: 3, Quantity: 100}})
	session.SetOffer("bob", []TradeItem{{TemplateID: 2, Quantity: 10}})
	session.Confirm("alice")
	session.Confirm("bob")

	if session.Status != TradeStatusConfirmed {
		t.Fatalf("want confirmed, got %s", session.Status)
	}
	if err := mgr.Execute(session); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if session.Status != TradeStatusCompleted {
		t.Fatalf("want completed, got %s", session.Status)
	}

	// 校验数量
	if invA.CountByTemplate(1) != 5 {
		t.Errorf("A HP Potion want 5, got %d", invA.CountByTemplate(1))
	}
	if invA.CountByTemplate(3) != 400 {
		t.Errorf("A Gold want 400, got %d", invA.CountByTemplate(3))
	}
	if invA.CountByTemplate(2) != 10 {
		t.Errorf("A MP Potion want 10, got %d", invA.CountByTemplate(2))
	}
	if invB.CountByTemplate(2) != 10 {
		t.Errorf("B MP Potion want 10, got %d", invB.CountByTemplate(2))
	}
	if invB.CountByTemplate(1) != 5 {
		t.Errorf("B HP Potion want 5, got %d", invB.CountByTemplate(1))
	}
	if invB.CountByTemplate(3) != 100 {
		t.Errorf("B Gold want 100, got %d", invB.CountByTemplate(3))
	}
}

func TestTrade_InsufficientItemsRolledBack(t *testing.T) {
	reg := setupTradeRegistry()
	invA := NewInventory(10, reg)
	invB := NewInventory(10, reg)
	invA.AddItem(1, 10)
	invB.AddItem(2, 3) // B 只有 3 MP Potion

	mgr := newTradeManager()
	session, _ := mgr.BeginTrade("alice", invA, "bob", invB)
	session.SetOffer("alice", []TradeItem{{TemplateID: 1, Quantity: 5}})
	session.SetOffer("bob", []TradeItem{{TemplateID: 2, Quantity: 10}}) // 不够
	session.Confirm("alice")
	session.Confirm("bob")

	err := mgr.Execute(session)
	if err == nil {
		t.Fatal("expected failure due to insufficient items")
	}
	if session.Status != TradeStatusCancelled {
		t.Fatalf("want cancelled (compensated), got %s: %v", session.Status, session.Error)
	}
	// 数量保持不变（全部回滚）
	if invA.CountByTemplate(1) != 10 {
		t.Errorf("A HP Potion should rollback to 10, got %d", invA.CountByTemplate(1))
	}
	if invB.CountByTemplate(2) != 3 {
		t.Errorf("B MP Potion should stay 3, got %d", invB.CountByTemplate(2))
	}
	// 不应残留对方道具
	if invA.CountByTemplate(2) != 0 || invB.CountByTemplate(1) != 0 {
		t.Errorf("no cross-items should exist, A-MP=%d B-HP=%d",
			invA.CountByTemplate(2), invB.CountByTemplate(1))
	}
}

// TestTrade_CreditFailureRollsBack 模拟在 credit 步骤因背包已满失败，整笔回滚
func TestTrade_CreditFailureRollsBack(t *testing.T) {
	reg := setupTradeRegistry()
	// A 只有 1 个槽位，且已满（被装备占用）
	invA := NewInventory(1, reg)
	invA.AddItem(4, 1) // Iron Sword，装备不可堆叠占用唯一槽位
	invB := NewInventory(10, reg)
	invB.AddItem(2, 5)

	mgr := newTradeManager()
	session, _ := mgr.BeginTrade("alice", invA, "bob", invB)
	// A 出剑换 B 的 MP Potion；A 背包收到 MP Potion 时无空槽位，credit_A 必定失败
	session.SetOffer("alice", []TradeItem{{TemplateID: 4, Quantity: 1}})
	session.SetOffer("bob", []TradeItem{{TemplateID: 2, Quantity: 5}})
	session.Confirm("alice")
	session.Confirm("bob")

	// A 的槽位数为 1，且被 Iron Sword 占用。debit_A 会先扣除 Iron Sword 空出槽位，
	// 因此 credit_A 实际会成功。为了真正触发 credit 失败，我们在 debit_A 之后再堆进一把剑
	// 此类场景较难模拟，改为用 B 背包满场景：
	invA2 := NewInventory(10, reg)
	invB2 := NewInventory(1, reg)
	invA2.AddItem(4, 1) // A 出剑
	invB2.AddItem(1, 50) // B 槽位被堆满（1 slot, 50 HP Potion 还可堆到 99，但槽位 1/1 已用）

	session2, _ := mgr.BeginTrade("alice", invA2, "bob", invB2)
	session2.SetOffer("alice", []TradeItem{{TemplateID: 4, Quantity: 1}})
	session2.SetOffer("bob", []TradeItem{{TemplateID: 1, Quantity: 10}}) // B 出 10 HP Potion
	session2.Confirm("alice")
	session2.Confirm("bob")

	err := mgr.Execute(session2)
	if err == nil {
		t.Fatal("expected credit failure (B has no free slot for Iron Sword)")
	}
	if session2.Status != TradeStatusCancelled {
		t.Fatalf("want cancelled, got %s: %v", session2.Status, session2.Error)
	}
	// 所有状态应回滚
	if invA2.CountByTemplate(4) != 1 {
		t.Errorf("A should keep Iron Sword, got %d", invA2.CountByTemplate(4))
	}
	if invB2.CountByTemplate(1) != 50 {
		t.Errorf("B should keep 50 HP Potion, got %d", invB2.CountByTemplate(1))
	}
	if invA2.CountByTemplate(1) != 0 {
		t.Errorf("A should not receive HP Potion, got %d", invA2.CountByTemplate(1))
	}
	if invB2.CountByTemplate(4) != 0 {
		t.Errorf("B should not receive Iron Sword, got %d", invB2.CountByTemplate(4))
	}
}

func TestTrade_TimeoutCancels(t *testing.T) {
	reg := setupTradeRegistry()
	invA := NewInventory(10, reg)
	invB := NewInventory(10, reg)

	mgr := NewTradeManager(saga.NewCoordinator(), 10*time.Millisecond)
	session, _ := mgr.BeginTrade("alice", invA, "bob", invB)

	// 不确认，等超时
	time.Sleep(20 * time.Millisecond)
	n := mgr.CleanupExpired(time.Now())
	if n != 1 {
		t.Fatalf("want 1 cancelled, got %d", n)
	}
	if session.Status != TradeStatusCancelled {
		t.Errorf("want cancelled, got %s", session.Status)
	}
}

func TestTrade_CancelBeforeExecute(t *testing.T) {
	mgr := newTradeManager()
	invA := NewInventory(10, setupTradeRegistry())
	invB := NewInventory(10, setupTradeRegistry())
	session, _ := mgr.BeginTrade("alice", invA, "bob", invB)
	if err := session.Cancel("user cancel"); err != nil {
		t.Fatal(err)
	}
	if session.Status != TradeStatusCancelled {
		t.Fatalf("want cancelled, got %s", session.Status)
	}
	if err := mgr.Execute(session); err == nil {
		t.Error("should refuse executing cancelled trade")
	}
}

func TestTrade_SetOfferResetsConfirm(t *testing.T) {
	mgr := newTradeManager()
	reg := setupTradeRegistry()
	invA := NewInventory(10, reg)
	invB := NewInventory(10, reg)
	invA.AddItem(1, 10)
	session, _ := mgr.BeginTrade("alice", invA, "bob", invB)
	session.SetOffer("alice", []TradeItem{{TemplateID: 1, Quantity: 2}})
	session.Confirm("alice")
	session.Confirm("bob")
	if session.Status != TradeStatusConfirmed {
		t.Fatal("prereq: both confirmed")
	}
	// 此时 Status 变为 Confirmed，SetOffer 应拒绝
	if err := session.SetOffer("alice", []TradeItem{{TemplateID: 1, Quantity: 3}}); err == nil {
		t.Error("should refuse SetOffer after confirm")
	}
}

func TestTrade_ConcurrentTradesOnSamePlayerSerialize(t *testing.T) {
	reg := setupTradeRegistry()
	invA := NewInventory(10, reg)
	invB := NewInventory(10, reg)
	invC := NewInventory(10, reg)
	invA.AddItem(1, 20)
	invB.AddItem(2, 10)
	invC.AddItem(2, 10)

	mgr := newTradeManager()

	// trade1: A ↔ B
	s1, _ := mgr.BeginTrade("alice", invA, "bob", invB)
	s1.SetOffer("alice", []TradeItem{{TemplateID: 1, Quantity: 5}})
	s1.SetOffer("bob", []TradeItem{{TemplateID: 2, Quantity: 5}})
	s1.Confirm("alice")
	s1.Confirm("bob")

	// trade2: A ↔ C
	s2, _ := mgr.BeginTrade("alice", invA, "carol", invC)
	s2.SetOffer("alice", []TradeItem{{TemplateID: 1, Quantity: 5}})
	s2.SetOffer("carol", []TradeItem{{TemplateID: 2, Quantity: 5}})
	s2.Confirm("alice")
	s2.Confirm("carol")

	var wg sync.WaitGroup
	wg.Add(2)
	var err1, err2 error
	go func() {
		defer wg.Done()
		err1 = mgr.Execute(s1)
	}()
	go func() {
		defer wg.Done()
		err2 = mgr.Execute(s2)
	}()
	wg.Wait()
	if err1 != nil || err2 != nil {
		t.Fatalf("both should succeed when serialized: %v %v", err1, err2)
	}
	if invA.CountByTemplate(1) != 10 {
		t.Errorf("A HP Potion want 10 (20-5-5), got %d", invA.CountByTemplate(1))
	}
	if invA.CountByTemplate(2) != 10 {
		t.Errorf("A MP Potion want 10 (5+5), got %d", invA.CountByTemplate(2))
	}
}

// 兜底：确保 validateOffers 对空 offer 的校验
func TestTrade_ValidateRejectsEmpty(t *testing.T) {
	reg := setupTradeRegistry()
	invA := NewInventory(10, reg)
	invB := NewInventory(10, reg)
	mgr := newTradeManager()
	s, _ := mgr.BeginTrade("alice", invA, "bob", invB)
	s.Confirm("alice")
	s.Confirm("bob")
	err := mgr.Execute(s)
	if err == nil || !errors.Is(err, err) {
		t.Error("expected error on empty trade")
	}
}
