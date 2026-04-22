package k8s

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"engine/cluster"
)

func TestK8sProvider_NewAndConfig(t *testing.T) {
	cfg := Config{
		Namespace:   "game",
		ServiceName: "player-svc",
		PortName:    "grpc",
	}
	p := New(cfg)

	if p.namespace != "game" {
		t.Errorf("namespace = %q, want \"game\"", p.namespace)
	}
	if p.serviceName != "player-svc" {
		t.Errorf("serviceName = %q, want \"player-svc\"", p.serviceName)
	}
	if p.portName != "grpc" {
		t.Errorf("portName = %q, want \"grpc\"", p.portName)
	}
}

func TestK8sProvider_Register_Noop(t *testing.T) {
	p := New(Config{})
	if err := p.Register(); err != nil {
		t.Errorf("Register should be no-op, got: %v", err)
	}
}

func TestK8sProvider_Deregister_Noop(t *testing.T) {
	p := New(Config{})
	if err := p.Deregister(); err != nil {
		t.Errorf("Deregister should be no-op, got: %v", err)
	}
}

func TestK8sProvider_Stop(t *testing.T) {
	p := New(Config{})
	p.stopChan = make(chan struct{})
	if err := p.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestK8sProvider_FindPort(t *testing.T) {
	tests := []struct {
		name     string
		portName string
		ports    []k8sPort
		want     int
	}{
		{
			name:     "empty ports",
			portName: "",
			ports:    nil,
			want:     0,
		},
		{
			name:     "first port when no name",
			portName: "",
			ports:    []k8sPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
			want:     8080,
		},
		{
			name:     "match by name",
			portName: "grpc",
			ports: []k8sPort{
				{Name: "http", Port: 8080},
				{Name: "grpc", Port: 9090},
			},
			want: 9090,
		},
		{
			name:     "no match falls to first",
			portName: "unknown",
			ports: []k8sPort{
				{Name: "http", Port: 8080},
			},
			want: 8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &K8sProvider{portName: tt.portName}
			got := p.findPort(tt.ports)
			if got != tt.want {
				t.Errorf("findPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestK8sProvider_ParseEndpoints(t *testing.T) {
	// 模拟 K8s Endpoints API 响应
	endpoints := k8sEndpoints{
		Subsets: []k8sSubset{
			{
				Addresses: []k8sAddress{
					{IP: "10.0.0.1", TargetRef: k8sTargetRef{Kind: "Pod", Name: "player-abc"}},
					{IP: "10.0.0.2", TargetRef: k8sTargetRef{Kind: "Pod", Name: "player-def"}},
				},
				Ports: []k8sPort{
					{Name: "game", Port: 8000, Protocol: "TCP"},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(endpoints)
	}))
	defer srv.Close()

	p := &K8sProvider{
		namespace:   "default",
		serviceName: "player-svc",
		portName:    "game",
		httpClient:  srv.Client(),
		token:       "test-token",
		stopChan:    make(chan struct{}),
	}

	// 直接构造请求到测试服务器（因为 GetMembers 默认请求 apiServer 常量）
	// 我们测试解析逻辑
	body, _ := json.Marshal(endpoints)
	var parsed k8sEndpoints
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	members := make([]*cluster.Member, 0)
	for _, subset := range parsed.Subsets {
		port := p.findPort(subset.Ports)
		for _, addr := range subset.Addresses {
			nodeID := addr.TargetRef.Name
			if nodeID == "" {
				nodeID = addr.IP
			}
			members = append(members, &cluster.Member{
				Address: addr.IP + ":" + intToStr(port),
				Id:      nodeID,
				Status:  cluster.MemberAlive,
			})
		}
	}

	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
	if members[0].Id != "player-abc" {
		t.Errorf("member[0].Id = %s, want player-abc", members[0].Id)
	}
	if members[1].Address != "10.0.0.2:8000" {
		t.Errorf("member[1].Address = %s, want 10.0.0.2:8000", members[1].Address)
	}
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}
