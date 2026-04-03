package etcd

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
		Kinds:   []string{"player"},
		Status:  cluster.MemberAlive,
	}
}

func TestEtcdProvider_RegisterAndDeregister(t *testing.T) {
	var leaseGranted, memberPut, memberDeleted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/lease/grant":
			leaseGranted = true
			json.NewEncoder(w).Encode(map[string]interface{}{"ID": "12345"})
		case "/v3/kv/put":
			memberPut = true
			w.WriteHeader(http.StatusOK)
		case "/v3/kv/deleterange":
			memberDeleted = true
			w.WriteHeader(http.StatusOK)
		case "/v3/lease/keepalive":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New(srv.URL)
	p.Start("test-cluster", newTestMember(), nil)

	if err := p.Register(); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if !leaseGranted {
		t.Error("expected lease grant")
	}
	if !memberPut {
		t.Error("expected member put")
	}

	if err := p.Deregister(); err != nil {
		t.Fatalf("Deregister failed: %v", err)
	}
	if !memberDeleted {
		t.Error("expected member delete")
	}

	p.Stop()
}

func TestEtcdProvider_GetMembers(t *testing.T) {
	info := memberInfo{Address: "10.0.0.1:8000", Id: "node-1", Kinds: []string{"player"}}
	infoBytes, _ := json.Marshal(info)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/kv/range" {
			resp := map[string]interface{}{
				"kvs": []map[string]string{
					{"value": encodeBase64(string(infoBytes))},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New(srv.URL)
	p.Start("test-cluster", newTestMember(), nil)
	defer p.Stop()

	members, err := p.GetMembers()
	if err != nil {
		t.Fatalf("GetMembers failed: %v", err)
	}

	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0].Address != "10.0.0.1:8000" {
		t.Errorf("address = %s, want 10.0.0.1:8000", members[0].Address)
	}
	if members[0].Id != "node-1" {
		t.Errorf("id = %s, want node-1", members[0].Id)
	}
}

func TestEtcdProvider_WatchLoop(t *testing.T) {
	info := memberInfo{Address: "10.0.0.1:8000", Id: "node-1", Kinds: []string{"player"}}
	infoBytes, _ := json.Marshal(info)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/kv/range" {
			resp := map[string]interface{}{
				"kvs": []map[string]string{
					{"value": encodeBase64(string(infoBytes))},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
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

	select {
	case members := <-changed:
		if len(members) != 1 {
			t.Errorf("expected 1 member, got %d", len(members))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for watch callback")
	}

	p.Stop()
}

func TestEncodeDecodeBase64(t *testing.T) {
	original := "/maplewish/clusters/test/members/node-1"
	encoded := encodeBase64(original)
	decoded := decodeBase64(encoded)

	if decoded != original {
		t.Errorf("round-trip failed: got %q, want %q", decoded, original)
	}
}

func TestDecodeBase64_Invalid(t *testing.T) {
	// 非法 base64 应返回原始字符串
	result := decodeBase64("not-valid-base64!!!")
	if result != "not-valid-base64!!!" {
		t.Errorf("expected original string back, got %q", result)
	}
}

func TestPrefixEnd(t *testing.T) {
	end := prefixEnd("/a/b/")
	if end != "/a/b0" { // '/' + 1 = '0'
		t.Errorf("prefixEnd(\"/a/b/\") = %q, want \"/a/b0\"", end)
	}
}

func TestNew_DefaultEndpoint(t *testing.T) {
	p := New()
	if len(p.endpoints) != 1 || p.endpoints[0] != "http://127.0.0.1:2379" {
		t.Errorf("unexpected default endpoint: %v", p.endpoints)
	}
}
