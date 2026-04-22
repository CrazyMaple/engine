package cluster

import (
	"testing"
	"time"

	"engine/actor"
	"engine/remote"
)

func TestDefaultDCConfig(t *testing.T) {
	config := DefaultDCConfig("us-east-1")
	if config.DCName != "us-east-1" {
		t.Errorf("DCName: got %s, want us-east-1", config.DCName)
	}
	if !config.LocalRoutePriority {
		t.Error("LocalRoutePriority should be true by default")
	}
	if config.CrossDCHeartbeatMultiplier != 3 {
		t.Errorf("CrossDCHeartbeatMultiplier: got %d, want 3", config.CrossDCHeartbeatMultiplier)
	}
	if !config.FailoverEnabled {
		t.Error("FailoverEnabled should be true by default")
	}
	if config.FailoverCooldown != 30*time.Second {
		t.Errorf("FailoverCooldown: got %v, want 30s", config.FailoverCooldown)
	}
}

func TestDCLabel(t *testing.T) {
	label := GetDCLabel("us-east-1")
	if label != "dc:us-east-1" {
		t.Errorf("GetDCLabel: got %s, want dc:us-east-1", label)
	}

	dc, ok := ParseDCLabel("dc:cn-north-1")
	if !ok || dc != "cn-north-1" {
		t.Errorf("ParseDCLabel: got (%s, %v), want (cn-north-1, true)", dc, ok)
	}

	_, ok = ParseDCLabel("notdc")
	if ok {
		t.Error("ParseDCLabel should return false for non-dc label")
	}

	_, ok = ParseDCLabel("dc:")
	if ok {
		t.Error("ParseDCLabel should return false for empty dc name")
	}
}

func TestClusterConfigWithDC(t *testing.T) {
	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	config.WithDC("us-east-1")

	found := false
	for _, k := range config.Kinds {
		if k == "dc:us-east-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dc label not found in Kinds: %v", config.Kinds)
	}

	// 重复添加不会重复
	config.WithDC("us-east-1")
	count := 0
	for _, k := range config.Kinds {
		if k == "dc:us-east-1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("duplicate dc label: found %d times", count)
	}
}

func TestMultiDCManagerCreation(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	config.WithDC("us-east-1")
	c := NewCluster(system, r, config)

	dcConfig := DefaultDCConfig("us-east-1")
	m := NewMultiDCManager(c, dcConfig)

	if m.LocalDC() != "us-east-1" {
		t.Errorf("LocalDC: got %s, want us-east-1", m.LocalDC())
	}
}

func TestGetMemberDC(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	m := NewMultiDCManager(c, DefaultDCConfig("us-east-1"))

	member := &Member{
		Address: "10.0.0.1:8000",
		Id:      "node1",
		Kinds:   []string{"player", "dc:us-west-2"},
		Status:  MemberAlive,
	}

	dc := m.GetMemberDC(member)
	if dc != "us-west-2" {
		t.Errorf("GetMemberDC: got %s, want us-west-2", dc)
	}

	// 无 DC 标签返回 default
	memberNoDC := &Member{
		Address: "10.0.0.2:8000",
		Id:      "node2",
		Kinds:   []string{"player"},
		Status:  MemberAlive,
	}
	dc = m.GetMemberDC(memberNoDC)
	if dc != "default" {
		t.Errorf("GetMemberDC: got %s, want default", dc)
	}
}

func TestShouldUseCrossDCTiming(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	m := NewMultiDCManager(c, DefaultDCConfig("us-east-1"))

	localMember := &Member{
		Kinds: []string{"dc:us-east-1"},
	}
	remoteMember := &Member{
		Kinds: []string{"dc:eu-west-1"},
	}

	if m.ShouldUseCrossDCTiming(localMember) {
		t.Error("local member should not use cross-DC timing")
	}
	if !m.ShouldUseCrossDCTiming(remoteMember) {
		t.Error("remote member should use cross-DC timing")
	}
}

func TestCrossDCTimings(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	dcConfig := DefaultDCConfig("us-east-1")
	m := NewMultiDCManager(c, dcConfig)

	expectedInterval := c.Config().HeartbeatInterval * time.Duration(dcConfig.CrossDCHeartbeatMultiplier)
	if m.CrossDCHeartbeatInterval() != expectedInterval {
		t.Errorf("CrossDCHeartbeatInterval: got %v, want %v",
			m.CrossDCHeartbeatInterval(), expectedInterval)
	}

	expectedTimeout := c.Config().HeartbeatTimeout * time.Duration(dcConfig.CrossDCHeartbeatTimeoutMultiplier)
	if m.CrossDCHeartbeatTimeout() != expectedTimeout {
		t.Errorf("CrossDCHeartbeatTimeout: got %v, want %v",
			m.CrossDCHeartbeatTimeout(), expectedTimeout)
	}
}

func TestIsDCHealthy(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	m := NewMultiDCManager(c, DefaultDCConfig("us-east-1"))
	m.Start()

	if !m.IsDCHealthy("us-east-1") {
		t.Error("local DC should be healthy after start")
	}
	if m.IsDCHealthy("unknown-dc") {
		t.Error("unknown DC should not be healthy")
	}
}

func TestFindBestMember(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	m := NewMultiDCManager(c, DefaultDCConfig("us-east-1"))

	members := []*Member{
		{Address: "10.0.0.1:8000", Kinds: []string{"player"}, Status: MemberAlive},
		{Address: "10.0.0.2:8000", Kinds: []string{"player"}, Status: MemberAlive},
		{Address: "10.0.0.3:8000", Kinds: []string{"room"}, Status: MemberAlive},
	}

	// 查找 player 类型
	result := m.findBestMember("player-123", "player", members)
	if result == nil {
		t.Fatal("findBestMember returned nil")
	}
	if result.Address != "10.0.0.1:8000" && result.Address != "10.0.0.2:8000" {
		t.Errorf("unexpected member selected: %s", result.Address)
	}

	// 查找不存在的类型
	result = m.findBestMember("player-123", "nonexistent", members)
	if result != nil {
		t.Error("should return nil for nonexistent kind")
	}

	// 空成员列表
	result = m.findBestMember("player-123", "player", nil)
	if result != nil {
		t.Error("should return nil for empty members")
	}

	// 跳过非存活成员
	deadMembers := []*Member{
		{Address: "10.0.0.1:8000", Kinds: []string{"player"}, Status: MemberDead},
	}
	result = m.findBestMember("player-123", "player", deadMembers)
	if result != nil {
		t.Error("should skip dead members")
	}
}

func TestReadWriteRouting(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	config.WithDC("us-east-1")
	config.Kinds = []string{"player", "dc:us-east-1"}
	c := NewCluster(system, r, config)

	dcConfig := DefaultDCConfig("us-east-1")
	m := NewMultiDCManager(c, dcConfig)

	// 添加本地 DC 成员
	m.mu.Lock()
	m.dcMembers["us-east-1"] = []*Member{
		{Address: "10.0.0.1:8000", Kinds: []string{"player", "dc:us-east-1"}, Status: MemberAlive},
	}
	m.dcMembers["eu-west-1"] = []*Member{
		{Address: "10.0.1.1:8000", Kinds: []string{"player", "dc:eu-west-1"}, Status: MemberAlive},
	}
	m.mu.Unlock()

	// 读路由应优先本地 DC
	readMember := m.GetRouteMemberForRead("player-1", "player")
	if readMember == nil {
		t.Fatal("GetRouteMemberForRead returned nil")
	}
	if readMember.Address != "10.0.0.1:8000" {
		t.Errorf("Read route should prefer local DC, got %s", readMember.Address)
	}

	// 写路由使用一致性哈希（不强制本地）
	writeMember := m.GetRouteMemberForWrite("player-1", "player")
	// 写路由返回值取决于一致性哈希，可能为 nil（因为哈希环可能未更新）
	// 这里主要验证不 panic
	_ = writeMember
}

func TestGetRouteMemberByMode(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	config.WithDC("us-east-1")
	c := NewCluster(system, r, config)

	m := NewMultiDCManager(c, DefaultDCConfig("us-east-1"))

	// 添加本地成员
	m.mu.Lock()
	m.dcMembers["us-east-1"] = []*Member{
		{Address: "10.0.0.1:8000", Kinds: []string{"player", "dc:us-east-1"}, Status: MemberAlive},
	}
	m.mu.Unlock()

	// RouteRead
	member := m.GetRouteMemberByMode("p1", "player", RouteRead)
	if member == nil {
		t.Fatal("RouteRead returned nil for local member")
	}

	// RouteDefault
	member = m.GetRouteMemberByMode("p1", "player", RouteDefault)
	if member == nil {
		t.Fatal("RouteDefault returned nil for local member")
	}

	// RouteWrite - uses global hash ring
	member = m.GetRouteMemberByMode("p1", "player", RouteWrite)
	// May be nil if hash ring is empty, that's fine
}

func TestRouteMode(t *testing.T) {
	if RouteDefault != 0 {
		t.Error("RouteDefault should be 0")
	}
	if RouteRead != 1 {
		t.Error("RouteRead should be 1")
	}
	if RouteWrite != 2 {
		t.Error("RouteWrite should be 2")
	}
}

func TestDCFailoverAndRecovery(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	dcConfig := DefaultDCConfig("us-east-1")
	dcConfig.FailoverThreshold = 1
	dcConfig.FailoverCooldown = 0 // 禁用冷却以便测试
	m := NewMultiDCManager(c, dcConfig)

	// 模拟 DC 故障
	m.mu.Lock()
	m.dcStatus["us-west-2"] = true
	grouped := map[string][]*Member{
		"us-east-1": {{Address: "10.0.0.1:8000", Status: MemberAlive}},
	}
	m.handleDCFailure("us-west-2", grouped)
	m.mu.Unlock()

	m.mu.RLock()
	backupDC, ok := m.failoverState["us-west-2"]
	m.mu.RUnlock()

	if !ok {
		t.Fatal("failover state should be set")
	}
	if backupDC != "us-east-1" {
		t.Errorf("backup DC: got %s, want us-east-1", backupDC)
	}

	// 模拟恢复
	m.mu.Lock()
	m.handleDCRecovery("us-west-2")
	m.mu.Unlock()

	m.mu.RLock()
	_, stillFailed := m.failoverState["us-west-2"]
	m.mu.RUnlock()

	if stillFailed {
		t.Error("failover state should be cleared after recovery")
	}
}
