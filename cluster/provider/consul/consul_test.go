package consul

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"engine/cluster"
)

func newTestMember() *cluster.Member {
	return &cluster.Member{
		Address: "127.0.0.1:8000",
		Id:      "node-1",
		Kinds:   []string{"player", "room"},
		Status:  cluster.MemberAlive,
	}
}

func TestConsulProvider_RegisterAndDeregister(t *testing.T) {
	var registered bool
	var deregistered bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/agent/service/register":
			registered = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/agent/service/deregister/node-1":
			deregistered = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New(srv.URL)
	p.Start("test-cluster", newTestMember(), nil)
	defer p.Stop()

	if err := p.Register(); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if !registered {
		t.Error("expected register request")
	}

	if err := p.Deregister(); err != nil {
		t.Fatalf("Deregister failed: %v", err)
	}
	if !deregistered {
		t.Error("expected deregister request")
	}
}

func TestConsulProvider_GetMembers(t *testing.T) {
	entries := []consulServiceEntry{
		{
			Service: consulService{
				ID:      "node-1",
				Service: "test-cluster",
				Address: "10.0.0.1",
				Port:    8000,
				Tags:    []string{"player"},
				Meta: map[string]string{
					"node_id": "node-1",
					"kinds":   "player,room",
				},
			},
		},
		{
			Service: consulService{
				ID:      "node-2",
				Service: "test-cluster",
				Address: "10.0.0.2",
				Port:    8001,
				Tags:    []string{"npc"},
				Meta:    map[string]string{},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	p := New(srv.URL)
	p.Start("test-cluster", newTestMember(), nil)
	defer p.Stop()

	members, err := p.GetMembers()
	if err != nil {
		t.Fatalf("GetMembers failed: %v", err)
	}

	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	// 第一个成员：有 meta 信息
	if members[0].Address != "10.0.0.1:8000" {
		t.Errorf("member[0] address = %s, want 10.0.0.1:8000", members[0].Address)
	}
	if members[0].Id != "node-1" {
		t.Errorf("member[0] id = %s, want node-1", members[0].Id)
	}
	if len(members[0].Kinds) != 2 || members[0].Kinds[0] != "player" {
		t.Errorf("member[0] kinds = %v, want [player room]", members[0].Kinds)
	}

	// 第二个成员：无 meta，回退到 Tags
	if members[1].Id != "node-2" {
		t.Errorf("member[1] id = %s, want node-2", members[1].Id)
	}
	if len(members[1].Kinds) != 1 || members[1].Kinds[0] != "npc" {
		t.Errorf("member[1] kinds = %v, want [npc]", members[1].Kinds)
	}
}

func TestConsulProvider_WatchServices(t *testing.T) {
	callCount := 0
	entries := []consulServiceEntry{
		{
			Service: consulService{
				ID:      "node-1",
				Address: "10.0.0.1",
				Port:    8000,
				Meta: map[string]string{
					"node_id": "node-1",
					"kinds":   "player",
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("X-Consul-Index", "42")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	changed := make(chan []*cluster.Member, 1)
	p := New(srv.URL)
	p.Start("test-cluster", newTestMember(), func(members []*cluster.Member) {
		select {
		case changed <- members:
		default:
		}
	})

	// 等待至少一次 watch 回调
	select {
	case members := <-changed:
		if len(members) != 1 {
			t.Errorf("expected 1 member, got %d", len(members))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for watch callback")
	}

	p.Stop()
}

func TestConsulProvider_RegisterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	p := New(srv.URL)
	p.Start("test-cluster", newTestMember(), nil)
	defer p.Stop()

	err := p.Register()
	if err == nil {
		t.Error("expected error on 500 response")
	}
}

func TestParseAddress(t *testing.T) {
	tests := []struct {
		addr string
		host string
		port int
	}{
		{"127.0.0.1:8080", "127.0.0.1", 8080},
		{"localhost:0", "localhost", 0},
		{"noport", "noport", 0},
	}

	for _, tt := range tests {
		host, port := parseAddress(tt.addr)
		if host != tt.host || port != tt.port {
			t.Errorf("parseAddress(%q) = (%q, %d), want (%q, %d)", tt.addr, host, port, tt.host, tt.port)
		}
	}
}

func TestParseKinds(t *testing.T) {
	if kinds := parseKinds(""); kinds != nil {
		t.Errorf("parseKinds(\"\") = %v, want nil", kinds)
	}

	kinds := parseKinds("a,b,c")
	if len(kinds) != 3 || kinds[0] != "a" || kinds[1] != "b" || kinds[2] != "c" {
		t.Errorf("parseKinds(\"a,b,c\") = %v", kinds)
	}
}

func TestNew_AutoPrefix(t *testing.T) {
	p := New("127.0.0.1:8500")
	if p.consulAddr != "http://127.0.0.1:8500" {
		t.Errorf("expected http prefix, got %s", p.consulAddr)
	}

	p2 := New("http://127.0.0.1:8500")
	if p2.consulAddr != "http://127.0.0.1:8500" {
		t.Errorf("should not double-prefix, got %s", p2.consulAddr)
	}
}
