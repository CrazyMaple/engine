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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ID": "12345"})
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
	self := newTestMember()
	if err := p.Start("test-cluster", self, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := p.Register(self); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if !leaseGranted {
		t.Error("expected lease grant")
	}
	if !memberPut {
		t.Error("expected member put")
	}

	if err := p.Deregister(self); err != nil {
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
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New(srv.URL)
	if err := p.Start("test-cluster", newTestMember(), nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
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
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	changed := make(chan []*cluster.Member, 1)
	p := New(srv.URL)
	if err := p.Start("test-cluster", newTestMember(), func(members []*cluster.Member) {
		select {
		case changed <- members:
		default:
		}
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

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
	if got := decodeBase64(encodeBase64(original)); got != original {
		t.Errorf("round-trip failed: got %q, want %q", got, original)
	}
}

func TestDecodeBase64_Invalid(t *testing.T) {
	if got := decodeBase64("not-valid-base64!!!"); got != "not-valid-base64!!!" {
		t.Errorf("expected original string back, got %q", got)
	}
}

func TestPrefixEnd(t *testing.T) {
	if got := prefixEnd("/a/b/"); got != "/a/b0" {
		t.Errorf("prefixEnd(\"/a/b/\") = %q, want \"/a/b0\"", got)
	}
}

func TestNew_DefaultEndpoint(t *testing.T) {
	p := New()
	if len(p.endpoints) != 1 || p.endpoints[0] != "http://127.0.0.1:2379" {
		t.Errorf("unexpected default endpoint: %v", p.endpoints)
	}
}
