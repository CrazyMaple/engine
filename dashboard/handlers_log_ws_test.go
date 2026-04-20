package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"engine/log"
)

// wsURL 把 http:// 测试服务器 URL 转成 ws:// 路径
func wsURL(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	u := strings.TrimPrefix(srv.URL, "http")
	return "ws" + u + path
}

// dialLogWS 连到测试服务器的 /ws/log
func dialLogWS(t *testing.T, srv *httptest.Server, rawQuery string) *websocket.Conn {
	t.Helper()
	u := wsURL(t, srv, "/ws/log")
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, _, err := dialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("ws dial %s: %v", u, err)
	}
	return conn
}

// readPayload 以短超时读一条消息；返回 payload 或超时则返回 nil
func readPayload(t *testing.T, conn *websocket.Conn, deadline time.Duration) *logWSPayload {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(deadline))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil
	}
	var p logWSPayload
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return &p
}

// waitSubscribers 等待 sink 完成订阅注册（异步 handler 生命周期）
func waitSubscribers(t *testing.T, bs *log.BroadcastSink, want int, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if bs.SubscriberCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("subscriber count: want %d, got %d", want, bs.SubscriberCount())
}

func newWSServer(cfg Config) *httptest.Server {
	h := &handlers{config: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/log", h.handleLogWS)
	return httptest.NewServer(mux)
}

// TestLogWS_NotConfigured 无 LogBroadcast 时返回 503
func TestLogWS_NotConfigured(t *testing.T) {
	srv := newWSServer(Config{})
	defer srv.Close()

	u := wsURL(t, srv, "/ws/log")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	_, resp, err := dialer.Dial(u, nil)
	if err == nil {
		t.Fatal("expected dial to fail without LogBroadcast")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, resp=%v err=%v", resp, err)
	}
}

// TestLogWS_EndToEnd 订阅后 Write 的日志应被推送到客户端
func TestLogWS_EndToEnd(t *testing.T) {
	bs := log.NewBroadcastSink()
	srv := newWSServer(Config{LogBroadcast: bs})
	defer srv.Close()

	conn := dialLogWS(t, srv, "")
	defer conn.Close()
	waitSubscribers(t, bs, 1, time.Second)

	now := time.Now()
	_ = bs.Write(log.LogEntry{Time: now, Level: log.LevelInfo, Msg: "hello"})

	p := readPayload(t, conn, 2*time.Second)
	if p == nil {
		t.Fatal("no payload received")
	}
	if p.Msg != "hello" {
		t.Fatalf("msg mismatch: got %q", p.Msg)
	}
	if p.Level != "INFO" {
		t.Fatalf("level mismatch: got %q", p.Level)
	}
}

// TestLogWS_FilterByTraceID 订阅时只收到匹配的 trace_id
func TestLogWS_FilterByTraceID(t *testing.T) {
	bs := log.NewBroadcastSink()
	srv := newWSServer(Config{LogBroadcast: bs})
	defer srv.Close()

	conn := dialLogWS(t, srv, "trace_id=t-want")
	defer conn.Close()
	waitSubscribers(t, bs, 1, time.Second)

	_ = bs.Write(log.LogEntry{Msg: "nope", TraceID: "t-other"})
	_ = bs.Write(log.LogEntry{Msg: "yes", TraceID: "t-want"})

	p := readPayload(t, conn, 2*time.Second)
	if p == nil {
		t.Fatal("no payload")
	}
	if p.TraceID != "t-want" || p.Msg != "yes" {
		t.Fatalf("expected t-want/yes, got %+v", p)
	}

	// 应该不会收到第二条（被过滤掉的）
	if p2 := readPayload(t, conn, 200*time.Millisecond); p2 != nil {
		t.Fatalf("unexpected extra payload: %+v", p2)
	}
}

// TestLogWS_FilterByLevel level 过滤
func TestLogWS_FilterByLevel(t *testing.T) {
	bs := log.NewBroadcastSink()
	srv := newWSServer(Config{LogBroadcast: bs})
	defer srv.Close()

	conn := dialLogWS(t, srv, "level=error")
	defer conn.Close()
	waitSubscribers(t, bs, 1, time.Second)

	_ = bs.Write(log.LogEntry{Msg: "info", Level: log.LevelInfo})
	_ = bs.Write(log.LogEntry{Msg: "warn", Level: log.LevelWarn})
	_ = bs.Write(log.LogEntry{Msg: "err1", Level: log.LevelError})

	p := readPayload(t, conn, 2*time.Second)
	if p == nil || p.Msg != "err1" {
		t.Fatalf("want err1, got %+v", p)
	}
}

// TestLogWS_FilterByMsg msg 子串过滤
func TestLogWS_FilterByMsg(t *testing.T) {
	bs := log.NewBroadcastSink()
	srv := newWSServer(Config{LogBroadcast: bs})
	defer srv.Close()

	conn := dialLogWS(t, srv, "msg=target")
	defer conn.Close()
	waitSubscribers(t, bs, 1, time.Second)

	_ = bs.Write(log.LogEntry{Msg: "skip this"})
	_ = bs.Write(log.LogEntry{Msg: "has target in it"})

	p := readPayload(t, conn, 2*time.Second)
	if p == nil || !strings.Contains(p.Msg, "target") {
		t.Fatalf("want msg with 'target', got %+v", p)
	}
}

// TestLogWS_Unsubscribe 客户端断开后应解除订阅
func TestLogWS_Unsubscribe(t *testing.T) {
	bs := log.NewBroadcastSink()
	srv := newWSServer(Config{LogBroadcast: bs})
	defer srv.Close()

	conn := dialLogWS(t, srv, "")
	waitSubscribers(t, bs, 1, time.Second)

	conn.Close()
	waitSubscribers(t, bs, 0, 2*time.Second)
}

// TestMatchWSFilter 覆盖 matchWSFilter 各分支
func TestMatchWSFilter(t *testing.T) {
	e := log.LogEntry{
		TraceID: "t1",
		NodeID:  "n1",
		Actor:   "/user/a",
		Level:   log.LevelInfo,
		Msg:     "hello world",
	}

	if !matchWSFilter(e, log.QueryFilter{}) {
		t.Fatal("empty filter should match")
	}
	if matchWSFilter(e, log.QueryFilter{TraceID: "other"}) {
		t.Fatal("trace_id mismatch should reject")
	}
	if matchWSFilter(e, log.QueryFilter{NodeID: "n2"}) {
		t.Fatal("node mismatch should reject")
	}
	if !matchWSFilter(e, log.QueryFilter{Actor: "user"}) {
		t.Fatal("actor substring should match")
	}
	if matchWSFilter(e, log.QueryFilter{MinLevel: log.LevelError}) {
		t.Fatal("below min level should reject")
	}
	if matchWSFilter(e, log.QueryFilter{MsgSubstr: "xyz"}) {
		t.Fatal("msg substr mismatch should reject")
	}
	if !matchWSFilter(e, log.QueryFilter{MsgSubstr: "hello"}) {
		t.Fatal("msg substr match should pass")
	}
}
