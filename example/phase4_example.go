//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"engine/actor"
	"engine/config"
	"engine/ecs"
	"engine/middleware"
	"engine/persistence"
	"engine/scene"
)

// ===== Phase 4 集成示例 =====
// 演示：场景管理 + AOI + ECS + 持久化 + 配置加载 + 中间件

// --- 配置表定义 ---

type MonsterConfig struct {
	ID   int    `rf:"index"`
	Name string
	HP   int
	ATK  int
}

// --- 可持久化玩家 Actor ---

type PlayerState struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
	HP    int    `json:"hp"`
}

type GamePlayerActor struct {
	id     string
	state  PlayerState
	entity *ecs.Entity
}

func (p *GamePlayerActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// 初始化 ECS 实体
		p.entity = ecs.NewEntity(p.id)
		p.entity.Add(&ecs.Position{X: 0, Y: 0})
		p.entity.Add(&ecs.Health{Current: p.state.HP, Max: 100})
		fmt.Printf("[player %s] started, state=%+v\n", p.id, p.state)

	case *scene.EntityEntered:
		fmt.Printf("[player %s] sees %s entered at (%.0f, %.0f)\n",
			p.id, msg.EntityID, msg.X, msg.Y)

	case *scene.EntityLeft:
		fmt.Printf("[player %s] sees %s left\n", p.id, msg.EntityID)

	case *scene.EntityMoved:
		fmt.Printf("[player %s] sees %s moved to (%.0f, %.0f)\n",
			p.id, msg.EntityID, msg.X, msg.Y)

	case string:
		fmt.Printf("[player %s] received: %s\n", p.id, msg)

	case *actor.Stopping:
		fmt.Printf("[player %s] stopping\n", p.id)
	}
}

func (p *GamePlayerActor) PersistenceID() string { return p.id }
func (p *GamePlayerActor) GetState() interface{} { return p.state }
func (p *GamePlayerActor) SetState(state interface{}) error {
	data, _ := json.Marshal(state)
	return json.Unmarshal(data, &p.state)
}

// RunPhase4Example 运行 Phase 4 集成示例
func RunPhase4Example() {
	fmt.Println("=== Phase 4 Game Engine Layer Example ===")
	fmt.Println()

	system := actor.DefaultSystem()
	metrics := middleware.NewMetrics()
	storage := persistence.NewMemoryStorage()

	// --- 1. 配置加载 ---
	fmt.Println("--- 1. Config Loading ---")
	dir, _ := os.MkdirTemp("", "phase4")
	defer os.RemoveAll(dir)

	// 创建怪物配置表
	monsterFile := filepath.Join(dir, "monsters.tsv")
	os.WriteFile(monsterFile, []byte("ID\tName\tHP\tATK\n1001\t史莱姆\t50\t10\n1002\t哥布林\t100\t25\n1003\t龙\t10000\t500\n"), 0644)

	mgr := config.NewManager()
	mgr.RegisterRecordFile(monsterFile, MonsterConfig{}, func() {
		fmt.Println("[config] monsters reloaded!")
	})
	if err := mgr.LoadAll(); err != nil {
		fmt.Printf("config load error: %v\n", err)
		return
	}

	entry := mgr.Get(monsterFile)
	for i := 0; i < entry.RecordFile.NumRecord(); i++ {
		m := entry.RecordFile.Record(i).(*MonsterConfig)
		fmt.Printf("  monster: ID=%d Name=%s HP=%d ATK=%d\n", m.ID, m.Name, m.HP, m.ATK)
	}
	fmt.Println()

	// --- 2. 创建带中间件的持久化玩家 ---
	fmt.Println("--- 2. Player with Middleware + Persistence ---")

	// 预存玩家数据
	storage.Save(context.Background(), "player-hero", PlayerState{Name: "Hero", Level: 10, HP: 80})

	p1Actor := &GamePlayerActor{id: "player-hero"}
	p1Props := actor.PropsFromProducer(func() actor.Actor {
		return p1Actor
	}).WithReceiverMiddleware(
		middleware.NewMetricsMiddleware(metrics),
		middleware.NewLogging(),
		persistence.NewPersistenceMiddleware(persistence.PersistenceConfig{
			Storage:      storage,
			SaveInterval: 5 * time.Second,
			SaveOnStop:   true,
			StateFactory: func() interface{} { return &PlayerState{} },
		}),
	)
	p1PID := system.Root.Spawn(p1Props)
	time.Sleep(100 * time.Millisecond)

	p2Actor := &GamePlayerActor{id: "player-mage", state: PlayerState{Name: "Mage", Level: 5, HP: 60}}
	p2Props := actor.PropsFromProducer(func() actor.Actor {
		return p2Actor
	}).WithReceiverMiddleware(
		middleware.NewMetricsMiddleware(metrics),
	)
	p2PID := system.Root.Spawn(p2Props)
	time.Sleep(100 * time.Millisecond)
	fmt.Println()

	// --- 3. 场景管理 + AOI ---
	fmt.Println("--- 3. Scene Management + AOI ---")

	sceneMgr := scene.NewSceneManager(system)
	scenePID := sceneMgr.CreateScene(scene.SceneConfig{
		SceneID: "new-village",
		GridConfig: scene.GridConfig{
			Width:    1000,
			Height:   1000,
			CellSize: 100,
		},
	})
	time.Sleep(50 * time.Millisecond)

	// 玩家进入场景
	system.Root.Send(scenePID, &scene.EnterScene{
		EntityID: "player-hero", PID: p1PID, X: 50, Y: 50,
	})
	system.Root.Send(scenePID, &scene.EnterScene{
		EntityID: "player-mage", PID: p2PID, X: 60, Y: 60,
	})
	time.Sleep(200 * time.Millisecond)

	// 玩家移动（跨格子触发 AOI）
	fmt.Println("\n--- Moving player-mage far away ---")
	system.Root.Send(scenePID, &scene.MoveInScene{
		EntityID: "player-mage", X: 500, Y: 500,
	})
	time.Sleep(200 * time.Millisecond)

	// 全场景广播
	fmt.Println("\n--- Scene broadcast ---")
	system.Root.Send(scenePID, &scene.BroadcastToScene{
		Message: "服务器即将维护",
	})
	time.Sleep(200 * time.Millisecond)

	// --- 4. 查看指标 ---
	fmt.Println("\n--- 4. Metrics Snapshot ---")
	snap := metrics.Snapshot()
	for t, c := range snap.MsgCount {
		fmt.Printf("  msg_type=%-30s count=%d\n", t, c)
	}

	// 清理
	fmt.Println("\n--- Cleanup ---")
	system.Root.Stop(p1PID)
	system.Root.Stop(p2PID)
	sceneMgr.RemoveScene("new-village")
	time.Sleep(200 * time.Millisecond)

	fmt.Println("\n=== Phase 4 Example Complete ===")
}
