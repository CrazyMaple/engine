package scene

import (
	"sync"

	"engine/actor"
	"engine/log"
)

// SceneLocator 场景定位接口，支持跨节点场景查找
type SceneLocator interface {
	// LocateScene 定位场景所在的 PID（可能在远程节点）
	LocateScene(sceneID string) (*actor.PID, bool)
}

// SceneManager 场景管理器，管理多个场景 Actor 的创建和查找
// 线程安全，可从多个 goroutine 调用
type SceneManager struct {
	system       *actor.ActorSystem
	scenes       map[string]*actor.PID
	remoteScenes map[string]*actor.PID // 已知的远程场景缓存
	locator      SceneLocator          // 可选，跨节点场景定位
	mu           sync.RWMutex
}

// NewSceneManager 创建场景管理器
func NewSceneManager(system *actor.ActorSystem) *SceneManager {
	return &SceneManager{
		system:       system,
		scenes:       make(map[string]*actor.PID),
		remoteScenes: make(map[string]*actor.PID),
	}
}

// SetLocator 设置跨节点场景定位器
func (sm *SceneManager) SetLocator(locator SceneLocator) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.locator = locator
}

// CreateScene 创建场景
func (sm *SceneManager) CreateScene(config SceneConfig) *actor.PID {
	producer := newSceneActorWithManager(config, sm)
	props := actor.PropsFromProducer(producer)
	pid := sm.system.Root.SpawnNamed(props, "scene/"+config.SceneID)

	sm.mu.Lock()
	sm.scenes[config.SceneID] = pid
	sm.mu.Unlock()

	return pid
}

// newSceneActorWithManager 创建携带 manager 引用的场景 Actor
func newSceneActorWithManager(config SceneConfig, manager *SceneManager) actor.Producer {
	return func() actor.Actor {
		return &SceneActor{config: config, manager: manager}
	}
}

// GetScene 获取场景 PID，先查本地再查远程缓存，最后通过 Locator 查找
func (sm *SceneManager) GetScene(sceneID string) (*actor.PID, bool) {
	sm.mu.RLock()
	// 本地场景
	if pid, ok := sm.scenes[sceneID]; ok {
		sm.mu.RUnlock()
		return pid, true
	}
	// 远程缓存
	if pid, ok := sm.remoteScenes[sceneID]; ok {
		sm.mu.RUnlock()
		return pid, true
	}
	locator := sm.locator
	sm.mu.RUnlock()

	// 通过定位器查找
	if locator != nil {
		if pid, ok := locator.LocateScene(sceneID); ok {
			sm.mu.Lock()
			sm.remoteScenes[sceneID] = pid
			sm.mu.Unlock()
			log.Info("[scene-mgr] located remote scene %s at %s", sceneID, pid.Address)
			return pid, true
		}
	}

	return nil, false
}

// RegisterRemoteScene 手动注册远程场景
func (sm *SceneManager) RegisterRemoteScene(sceneID string, pid *actor.PID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.remoteScenes[sceneID] = pid
}

// UnregisterRemoteScene 注销远程场景
func (sm *SceneManager) UnregisterRemoteScene(sceneID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.remoteScenes, sceneID)
}

// RemoveScene 停止并移除场景
func (sm *SceneManager) RemoveScene(sceneID string) {
	sm.mu.Lock()
	pid, ok := sm.scenes[sceneID]
	if ok {
		delete(sm.scenes, sceneID)
	}
	sm.mu.Unlock()

	if ok {
		sm.system.Root.Stop(pid)
	}
}

// SceneCount 场景数量（仅本地）
func (sm *SceneManager) SceneCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.scenes)
}

// AllSceneIDs 返回所有本地场景 ID
func (sm *SceneManager) AllSceneIDs() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ids := make([]string, 0, len(sm.scenes))
	for id := range sm.scenes {
		ids = append(ids, id)
	}
	return ids
}

// TransferEntity 便捷方法：发起跨场景实体转移
func (sm *SceneManager) TransferEntity(entityID, sourceSceneID, targetSceneID string, targetX, targetY float32) bool {
	sourcePID, ok := sm.GetScene(sourceSceneID)
	if !ok {
		return false
	}
	sm.system.Root.Send(sourcePID, &TransferEntity{
		EntityID:      entityID,
		TargetSceneID: targetSceneID,
		TargetX:       targetX,
		TargetY:       targetY,
	})
	return true
}
