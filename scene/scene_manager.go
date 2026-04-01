package scene

import (
	"sync"

	"engine/actor"
)

// SceneManager 场景管理器，管理多个场景 Actor 的创建和查找
// 线程安全，可从多个 goroutine 调用
type SceneManager struct {
	system *actor.ActorSystem
	scenes map[string]*actor.PID
	mu     sync.RWMutex
}

// NewSceneManager 创建场景管理器
func NewSceneManager(system *actor.ActorSystem) *SceneManager {
	return &SceneManager{
		system: system,
		scenes: make(map[string]*actor.PID),
	}
}

// CreateScene 创建场景
func (sm *SceneManager) CreateScene(config SceneConfig) *actor.PID {
	props := actor.PropsFromProducer(NewSceneActor(config))
	pid := sm.system.Root.SpawnNamed(props, "scene/"+config.SceneID)

	sm.mu.Lock()
	sm.scenes[config.SceneID] = pid
	sm.mu.Unlock()

	return pid
}

// GetScene 获取场景 PID
func (sm *SceneManager) GetScene(sceneID string) (*actor.PID, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	pid, ok := sm.scenes[sceneID]
	return pid, ok
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

// SceneCount 场景数量
func (sm *SceneManager) SceneCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.scenes)
}
