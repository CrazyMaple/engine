package cluster

import (
	"fmt"
	"sync"
	"time"

	"engine/actor"
	"engine/log"
	"engine/remote"
)

// Migratable Actor 实现此接口以支持 Live Migration
type Migratable interface {
	// MarshalState 序列化 Actor 状态
	MarshalState() ([]byte, error)
	// UnmarshalState 反序列化 Actor 状态
	UnmarshalState(data []byte) error
}

// MigrationAware Actor 可选实现此接口以接收迁移生命周期通知
type MigrationAware interface {
	// OnMigrationStart 迁移开始时调用（源节点）
	OnMigrationStart()
	// OnMigrationComplete 迁移完成时调用（目标节点恢复后）
	OnMigrationComplete()
}

// MigrationStatus 迁移状态
type MigrationStatus int

const (
	MigrationPending    MigrationStatus = iota // 等待开始
	MigrationPausing                           // 暂停消息处理
	MigrationSerializing                       // 序列化状态
	MigrationTransferring                      // 传输中
	MigrationRestoring                         // 目标节点恢复中
	MigrationCompleted                         // 完成
	MigrationFailed                            // 失败
	MigrationRolledBack                        // 已回滚
)

func (s MigrationStatus) String() string {
	switch s {
	case MigrationPending:
		return "Pending"
	case MigrationPausing:
		return "Pausing"
	case MigrationSerializing:
		return "Serializing"
	case MigrationTransferring:
		return "Transferring"
	case MigrationRestoring:
		return "Restoring"
	case MigrationCompleted:
		return "Completed"
	case MigrationFailed:
		return "Failed"
	case MigrationRolledBack:
		return "RolledBack"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// MigrationConfig 迁移配置
type MigrationConfig struct {
	// Timeout 迁移超时时间，超时自动回滚
	Timeout time.Duration
	// PauseTimeout 暂停等待时间
	PauseTimeout time.Duration
}

// DefaultMigrationConfig 默认迁移配置
func DefaultMigrationConfig() *MigrationConfig {
	return &MigrationConfig{
		Timeout:      30 * time.Second,
		PauseTimeout: 5 * time.Second,
	}
}

// --- 迁移协议消息 ---

// MigrateRequest 迁移请求（发送到源节点的迁移协调器）
type MigrateRequest struct {
	ActorID    string // 源 Actor 的 ID
	TargetAddr string // 目标节点地址
	Props      *actor.Props // 目标节点用于 Spawn 的 Props（需预先在目标注册）
	KindName   string // Actor Kind 名称（用于目标节点查找 Props）
	Timeout    time.Duration
}

// MigrateResponse 迁移响应
type MigrateResponse struct {
	Success bool
	NewPID  *actor.PID // 目标节点上的新 PID
	Error   string
}

// MigrationPause 暂停 Actor 处理消息（系统消息）
type MigrationPause struct{}

// MigrationPauseAck Actor 已暂停确认
type MigrationPauseAck struct {
	ActorID string
}

// MigrationSerialize 请求 Actor 序列化状态
type MigrationSerialize struct{}

// MigrationStateData 序列化的 Actor 状态数据
type MigrationStateData struct {
	ActorID  string
	KindName string
	State    []byte // 序列化后的状态
}

// MigrationRestore 在目标节点恢复 Actor 状态
type MigrationRestore struct {
	ActorID  string
	KindName string
	State    []byte
}

// MigrationRestoreAck 目标节点恢复确认
type MigrationRestoreAck struct {
	PID   *actor.PID
	Error string
}

// MigrationResume 恢复 Actor 消息处理（回滚时使用）
type MigrationResume struct{}

// MigrationStartNotify 通知 Actor 迁移即将开始（触发 MigrationAware.OnMigrationStart）
type MigrationStartNotify struct{}

// MigrationCompleteNotify 通知 Actor 迁移已完成（触发 MigrationAware.OnMigrationComplete）
type MigrationCompleteNotify struct{}

// MigrationComplete 迁移完成通知
type MigrationComplete struct {
	OldPID *actor.PID
	NewPID *actor.PID
}

// --- 迁移事件 ---

// MigrationStartedEvent 迁移开始事件
type MigrationStartedEvent struct {
	ActorID    string
	SourceAddr string
	TargetAddr string
}

// MigrationCompletedEvent 迁移完成事件
type MigrationCompletedEvent struct {
	ActorID    string
	SourceAddr string
	TargetAddr string
	NewPID     *actor.PID
	Duration   time.Duration
}

// MigrationFailedEvent 迁移失败事件
type MigrationFailedEvent struct {
	ActorID    string
	SourceAddr string
	TargetAddr string
	Reason     string
}

// --- 迁移管理器 ---

// MigrationManager 管理 Actor 迁移
type MigrationManager struct {
	cluster    *Cluster
	config     *MigrationConfig
	coordPID   *actor.PID // 迁移协调器 Actor
	redirects  map[string]*actor.PID // 旧 ActorID -> 新 PID（消息转发表）
	kindProps  map[string]*actor.Props // KindName -> Props（用于目标节点 Spawn）
	singleton  *ClusterSingleton // 可选：关联的 Singleton 管理器（用于联动迁移）
	mu         sync.RWMutex
}

// NewMigrationManager 创建迁移管理器
func NewMigrationManager(c *Cluster, config *MigrationConfig) *MigrationManager {
	if config == nil {
		config = DefaultMigrationConfig()
	}
	return &MigrationManager{
		cluster:   c,
		config:    config,
		redirects: make(map[string]*actor.PID),
		kindProps: make(map[string]*actor.Props),
	}
}

// SetSingleton 关联 ClusterSingleton 管理器（Singleton 迁移联动）
func (mm *MigrationManager) SetSingleton(cs *ClusterSingleton) {
	mm.singleton = cs
}

// MigrateSingleton 迁移集群单例到目标节点
// 通过 ClusterSingleton 获取当前 PID，迁移后在目标节点重新激活
func (mm *MigrationManager) MigrateSingleton(kind string, targetAddr string) (*actor.PID, error) {
	if mm.singleton == nil {
		return nil, fmt.Errorf("no ClusterSingleton associated with MigrationManager")
	}

	// 获取当前 Singleton PID
	pid, err := mm.singleton.Get(kind)
	if err != nil {
		return nil, fmt.Errorf("get singleton %s: %w", kind, err)
	}

	// 确认当前 Singleton 在本节点
	if pid.Address != "" && pid.Address != mm.cluster.Self().Address {
		return nil, fmt.Errorf("singleton %s is on %s, not local node %s",
			kind, pid.Address, mm.cluster.Self().Address)
	}

	// 注销本地 Singleton（迁移完成后在目标节点重新激活）
	mm.singleton.Unregister(kind)

	// 执行迁移
	newPID, err := mm.Migrate(pid, targetAddr)
	if err != nil {
		// 迁移失败，重新注册 Singleton
		mm.singleton.mu.RLock()
		entry, hasEntry := mm.singleton.singletons[kind]
		mm.singleton.mu.RUnlock()
		if !hasEntry || entry == nil {
			// 重新注册（需要 Props）
			if props, ok := mm.getKindProps(kind); ok {
				_ = mm.singleton.Register(SingletonConfig{
					Kind:  kind,
					Props: props,
				})
			}
		}
		return nil, fmt.Errorf("migrate singleton %s: %w", kind, err)
	}

	log.Info("Singleton %s migrated to %s (new PID: %s)", kind, targetAddr, newPID.String())
	return newPID, nil
}

// RegisterKind 注册可迁移的 Actor Kind 及其 Props
func (mm *MigrationManager) RegisterKind(kindName string, props *actor.Props) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.kindProps[kindName] = props
}

// Start 启动迁移管理器
func (mm *MigrationManager) Start() {
	// 注册远程消息类型
	remote.RegisterType(&MigrateRequest{})
	remote.RegisterType(&MigrateResponse{})
	remote.RegisterType(&MigrationStateData{})
	remote.RegisterType(&MigrationRestore{})
	remote.RegisterType(&MigrationRestoreAck{})
	remote.RegisterType(&MigrationComplete{})

	// 启动迁移协调器 Actor
	props := actor.PropsFromProducer(func() actor.Actor {
		return &migrationCoordActor{manager: mm}
	})
	mm.coordPID = mm.cluster.System().Root.SpawnNamed(props, "cluster/migration")

	log.Info("MigrationManager started on %s", mm.cluster.Self().Address)
}

// Stop 停止迁移管理器
func (mm *MigrationManager) Stop() {
	if mm.coordPID != nil {
		mm.cluster.System().Root.Stop(mm.coordPID)
	}
}

// Migrate 发起 Actor 迁移
func (mm *MigrationManager) Migrate(actorPID *actor.PID, targetAddr string) (*actor.PID, error) {
	if actorPID == nil {
		return nil, fmt.Errorf("actor PID cannot be nil")
	}
	if targetAddr == "" {
		return nil, fmt.Errorf("target address cannot be empty")
	}
	if targetAddr == mm.cluster.Self().Address {
		return nil, fmt.Errorf("cannot migrate to self")
	}

	// 通过 RequestFuture 发送迁移请求到协调器
	timeout := mm.config.Timeout
	future := mm.cluster.System().Root.RequestFuture(mm.coordPID, &MigrateRequest{
		ActorID:    actorPID.Id,
		TargetAddr: targetAddr,
		Timeout:    timeout,
	}, timeout+5*time.Second) // 额外留 5s 给协调器响应

	result, err := future.Wait()
	if err != nil {
		return nil, fmt.Errorf("migration request failed: %w", err)
	}

	resp, ok := result.(*MigrateResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", result)
	}

	if !resp.Success {
		return nil, fmt.Errorf("migration failed: %s", resp.Error)
	}

	return resp.NewPID, nil
}

// GetRedirect 查找消息转发目标（迁移后旧 PID 的消息会转发到新 PID）
func (mm *MigrationManager) GetRedirect(actorID string) (*actor.PID, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	pid, ok := mm.redirects[actorID]
	return pid, ok
}

// addRedirect 添加消息转发规则
func (mm *MigrationManager) addRedirect(actorID string, newPID *actor.PID) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.redirects[actorID] = newPID
}

// removeRedirect 移除消息转发规则
func (mm *MigrationManager) removeRedirect(actorID string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	delete(mm.redirects, actorID)
}

// getKindProps 获取 Kind 的 Props
func (mm *MigrationManager) getKindProps(kindName string) (*actor.Props, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	p, ok := mm.kindProps[kindName]
	return p, ok
}

// --- 迁移协调器 Actor ---

type migrationCoordActor struct {
	manager    *MigrationManager
	migrations map[string]*migrationTask // actorID -> task
}

type migrationTask struct {
	request   *MigrateRequest
	sender    *actor.PID // 请求者（用于回复结果）
	status    MigrationStatus
	stateData []byte
	startTime time.Time
	timer     *time.Timer
}

func (a *migrationCoordActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		a.migrations = make(map[string]*migrationTask)

	case *MigrateRequest:
		a.handleMigrateRequest(ctx, msg)

	case *MigrationStateData:
		a.handleStateData(ctx, msg)

	case *MigrationRestoreAck:
		a.handleRestoreAck(ctx, msg)

	case *MigrationRestore:
		// 作为目标节点收到恢复请求
		a.handleRestoreOnTarget(ctx, msg)

	case *migrationTimeout:
		a.handleTimeout(ctx, msg)
	}
}

func (a *migrationCoordActor) handleMigrateRequest(ctx actor.Context, msg *MigrateRequest) {
	actorID := msg.ActorID

	// 检查是否已有进行中的迁移
	if _, exists := a.migrations[actorID]; exists {
		ctx.Respond(&MigrateResponse{
			Success: false,
			Error:   "migration already in progress for " + actorID,
		})
		return
	}

	// 检查 Actor 是否存在
	proc, ok := a.manager.cluster.System().ProcessRegistry.GetByID(actorID)
	if !ok {
		ctx.Respond(&MigrateResponse{
			Success: false,
			Error:   "actor not found: " + actorID,
		})
		return
	}

	task := &migrationTask{
		request:   msg,
		sender:    ctx.Sender(),
		status:    MigrationPausing,
		startTime: time.Now(),
	}

	// 设置超时保护
	timeout := msg.Timeout
	if timeout <= 0 {
		timeout = a.manager.config.Timeout
	}
	task.timer = time.AfterFunc(timeout, func() {
		// 超时回滚
		ctx.Send(ctx.Self(), &migrationTimeout{actorID: actorID})
	})

	a.migrations[actorID] = task

	// 发布迁移开始事件
	a.manager.cluster.System().EventStream.Publish(&MigrationStartedEvent{
		ActorID:    actorID,
		SourceAddr: a.manager.cluster.Self().Address,
		TargetAddr: msg.TargetAddr,
	})

	// Step 1: 通知 Actor 迁移即将开始（触发 MigrationAware.OnMigrationStart）
	actorPID := actor.NewLocalPID(actorID)
	proc.SendUserMessage(actorPID, actor.WrapEnvelope(&MigrationStartNotify{}, ctx.Self()))

	// Step 2: 向 Actor 发送序列化请求
	// 通过 MigrationSerialize 消息让 Actor 自行序列化并返回状态
	proc.SendUserMessage(actorPID, actor.WrapEnvelope(&MigrationSerialize{}, ctx.Self()))

	log.Info("Migration started: %s -> %s", actorID, msg.TargetAddr)
}

func (a *migrationCoordActor) handleStateData(ctx actor.Context, msg *MigrationStateData) {
	task, ok := a.migrations[msg.ActorID]
	if !ok {
		return
	}

	task.status = MigrationTransferring
	task.stateData = msg.State

	// Step 2: 将状态数据发送到目标节点的迁移协调器
	targetCoord := actor.NewPID(task.request.TargetAddr, "cluster/migration")
	ctx.Send(targetCoord, &MigrationRestore{
		ActorID:  msg.ActorID,
		KindName: msg.KindName,
		State:    msg.State,
	})

	log.Info("Migration transferring state: %s (%d bytes) -> %s",
		msg.ActorID, len(msg.State), task.request.TargetAddr)
}

func (a *migrationCoordActor) handleRestoreOnTarget(ctx actor.Context, msg *MigrationRestore) {
	// 在目标节点上恢复 Actor
	props, ok := a.manager.getKindProps(msg.KindName)
	if !ok {
		ctx.Respond(&MigrationRestoreAck{
			Error: "unknown kind: " + msg.KindName,
		})
		return
	}

	// Spawn 新 Actor
	pid := ctx.Spawn(props)

	// 发送状态恢复消息
	ctx.Send(pid, &MigrationRestore{
		ActorID:  msg.ActorID,
		KindName: msg.KindName,
		State:    msg.State,
	})

	ctx.Respond(&MigrationRestoreAck{PID: pid})

	log.Info("Migration restored actor: %s -> %s", msg.ActorID, pid.String())
}

func (a *migrationCoordActor) handleRestoreAck(ctx actor.Context, msg *MigrationRestoreAck) {
	// 找到对应的迁移任务
	var task *migrationTask
	var actorID string
	for id, t := range a.migrations {
		if t.status == MigrationTransferring {
			task = t
			actorID = id
			break
		}
	}
	if task == nil {
		return
	}

	if msg.Error != "" {
		// 恢复失败，回滚
		a.rollback(ctx, actorID, task, msg.Error)
		return
	}

	task.timer.Stop()
	task.status = MigrationCompleted

	// Step 3: 通知目标节点上的新 Actor 迁移完成（触发 MigrationAware.OnMigrationComplete）
	ctx.Send(msg.PID, &MigrationCompleteNotify{})

	// Step 4: 停止源 Actor 并设置消息转发
	oldPID := actor.NewLocalPID(actorID)
	a.manager.cluster.System().Root.Stop(oldPID)

	// 添加消息转发规则
	a.manager.addRedirect(actorID, msg.PID)

	// 发布完成事件
	duration := time.Since(task.startTime)
	a.manager.cluster.System().EventStream.Publish(&MigrationCompletedEvent{
		ActorID:    actorID,
		SourceAddr: a.manager.cluster.Self().Address,
		TargetAddr: task.request.TargetAddr,
		NewPID:     msg.PID,
		Duration:   duration,
	})

	// 回复请求者
	if task.sender != nil {
		ctx.Send(task.sender, &MigrateResponse{
			Success: true,
			NewPID:  msg.PID,
		})
	}

	delete(a.migrations, actorID)

	log.Info("Migration completed: %s -> %s (took %v)",
		actorID, msg.PID.String(), duration)
}

func (a *migrationCoordActor) rollback(ctx actor.Context, actorID string, task *migrationTask, reason string) {
	task.timer.Stop()
	task.status = MigrationRolledBack

	// 向源 Actor 发送恢复消息
	actorPID := actor.NewLocalPID(actorID)
	proc, ok := a.manager.cluster.System().ProcessRegistry.GetByID(actorID)
	if ok {
		proc.SendUserMessage(actorPID, &MigrationResume{})
	}

	// 发布失败事件
	a.manager.cluster.System().EventStream.Publish(&MigrationFailedEvent{
		ActorID:    actorID,
		SourceAddr: a.manager.cluster.Self().Address,
		TargetAddr: task.request.TargetAddr,
		Reason:     reason,
	})

	// 回复请求者
	if task.sender != nil {
		ctx.Send(task.sender, &MigrateResponse{
			Success: false,
			Error:   "migration failed: " + reason,
		})
	}

	delete(a.migrations, actorID)

	log.Info("Migration rolled back: %s, reason: %s", actorID, reason)
}

func (a *migrationCoordActor) handleTimeout(ctx actor.Context, msg *migrationTimeout) {
	task, ok := a.migrations[msg.actorID]
	if !ok {
		return
	}
	a.rollback(ctx, msg.actorID, task, "migration timeout")
}

// migrationTimeout 迁移超时内部消息
type migrationTimeout struct {
	actorID string
}

// --- 消息转发进程 ---

// RedirectProcess 消息转发进程，将发往已迁移 Actor 的消息转发到新地址
type RedirectProcess struct {
	manager *MigrationManager
}

// NewRedirectProcess 创建消息转发进程
func NewRedirectProcess(mm *MigrationManager) *RedirectProcess {
	return &RedirectProcess{manager: mm}
}

// SendUserMessage 转发用户消息
func (rp *RedirectProcess) SendUserMessage(pid *actor.PID, message interface{}) {
	if newPID, ok := rp.manager.GetRedirect(pid.Id); ok {
		proc, found := rp.manager.cluster.System().ProcessRegistry.Get(newPID)
		if found {
			proc.SendUserMessage(newPID, message)
			return
		}
	}
	// 找不到转发目标，发到 dead letter
	dl := rp.manager.cluster.System().DeadLetter
	if dlProc, ok := rp.manager.cluster.System().ProcessRegistry.Get(dl); ok {
		dlProc.SendUserMessage(dl, message)
	}
}

// SendSystemMessage 转发系统消息
func (rp *RedirectProcess) SendSystemMessage(pid *actor.PID, message interface{}) {
	if newPID, ok := rp.manager.GetRedirect(pid.Id); ok {
		proc, found := rp.manager.cluster.System().ProcessRegistry.Get(newPID)
		if found {
			proc.SendSystemMessage(newPID, message)
			return
		}
	}
	dl := rp.manager.cluster.System().DeadLetter
	if dlProc, ok := rp.manager.cluster.System().ProcessRegistry.Get(dl); ok {
		dlProc.SendSystemMessage(dl, message)
	}
}

// Stop 转发停止
func (rp *RedirectProcess) Stop(pid *actor.PID) {
	if newPID, ok := rp.manager.GetRedirect(pid.Id); ok {
		proc, found := rp.manager.cluster.System().ProcessRegistry.Get(newPID)
		if found {
			proc.Stop(newPID)
			return
		}
	}
}
