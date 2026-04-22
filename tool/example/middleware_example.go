//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"engine/actor"
	"tool/dashboard"
	"gamelib/middleware"
	"gamelib/persistence"
)

// ===== 中间件组合示例 =====
// 演示：日志 + 指标 + 热点追踪 + 持久化中间件的堆叠使用

// BankAccount 可持久化的银行账户 Actor
type BankAccount struct {
	accountID string
	balance   int
}

type Deposit struct{ Amount int }
type Withdraw struct{ Amount int }
type QueryBalance struct{}

func (b *BankAccount) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		fmt.Printf("[%s] Account started, balance: %d\n", b.accountID, b.balance)

	case *Deposit:
		b.balance += msg.Amount
		fmt.Printf("[%s] Deposit %d, balance: %d\n", b.accountID, msg.Amount, b.balance)

	case *Withdraw:
		if b.balance >= msg.Amount {
			b.balance -= msg.Amount
			fmt.Printf("[%s] Withdraw %d, balance: %d\n", b.accountID, msg.Amount, b.balance)
		} else {
			fmt.Printf("[%s] Insufficient funds (need %d, have %d)\n", b.accountID, msg.Amount, b.balance)
		}

	case *QueryBalance:
		fmt.Printf("[%s] Balance query: %d\n", b.accountID, b.balance)
		ctx.Respond(b.balance)

	case *actor.Stopping:
		fmt.Printf("[%s] Account stopping, final balance: %d\n", b.accountID, b.balance)
	}
}

func (b *BankAccount) PersistenceID() string     { return b.accountID }
func (b *BankAccount) GetState() interface{}      { return b.balance }
func (b *BankAccount) SetState(state interface{}) error {
	data, _ := json.Marshal(state)
	return json.Unmarshal(data, &b.balance)
}

func RunMiddlewareExample() {
	fmt.Println("===== Middleware Composition Example =====")
	fmt.Println()

	system := actor.DefaultSystem()

	// 创建各个中间件组件
	metrics := middleware.NewMetrics()
	hotTracker := dashboard.NewHotActorTracker()
	storage := persistence.NewMemoryStorage()

	// 预存一个账户
	storage.Save(context.Background(), "account-alice", 1000)

	// 创建 Actor，堆叠多层中间件
	// 执行顺序（从外到内）：日志 → 指标 → 热点追踪 → 持久化 → Actor
	aliceProps := actor.PropsFromProducer(func() actor.Actor {
		return &BankAccount{accountID: "account-alice"}
	}).WithReceiverMiddleware(
		middleware.NewLogging(),                            // 1. 日志：记录消息类型和处理耗时
		middleware.NewMetricsMiddleware(metrics),            // 2. 指标：统计消息计数和延迟
		dashboard.NewHotActorMiddleware(hotTracker),         // 3. 热点：追踪高频 Actor
		persistence.NewPersistenceMiddleware(persistence.PersistenceConfig{
			Storage:      storage,
			SaveInterval: 5 * time.Second,
			SaveOnStop:   true,
			StateFactory: func() interface{} { return new(int) },
		}), // 4. 持久化：自动保存/恢复状态
	)

	alicePID := system.Root.Spawn(aliceProps)
	time.Sleep(100 * time.Millisecond)

	// 发送一些操作
	system.Root.Send(alicePID, &Deposit{Amount: 500})
	system.Root.Send(alicePID, &Withdraw{Amount: 200})
	system.Root.Send(alicePID, &QueryBalance{})
	system.Root.Send(alicePID, &Deposit{Amount: 100})
	time.Sleep(200 * time.Millisecond)

	// 查看指标
	fmt.Println("\n--- Metrics Snapshot ---")
	snap := metrics.Snapshot()
	for t, c := range snap.MsgCount {
		fmt.Printf("  %-35s count=%d\n", t, c)
	}

	// 查看热点
	fmt.Println("\n--- Hot Actors ---")
	top := hotTracker.TopN(5)
	for _, s := range top {
		fmt.Printf("  PID=%-20s msgs=%d avg_lat=%.2fms\n", s.PID, s.MsgCount, float64(s.AvgLatNs)/1e6)
	}

	// 停止（触发持久化保存）
	fmt.Println("\n--- Stopping (auto-persist) ---")
	system.Root.Stop(alicePID)
	time.Sleep(200 * time.Millisecond)

	fmt.Println("\n===== Example Complete =====")
}
