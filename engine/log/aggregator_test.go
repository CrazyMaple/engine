package log

import (
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileLogSink_WriteAppendsJSONLine(t *testing.T) {
	var buf bytes.Buffer
	sink := NewWriterSink(&buf)
	defer sink.Close()

	entry := LogEntry{
		Time:    time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
		Level:   LevelInfo,
		Msg:     "hello",
		NodeID:  "n1",
		TraceID: "trace-1",
		Actor:   "/user/foo",
		Fields:  map[string]interface{}{"k": "v"},
	}
	if err := sink.Write(entry); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("expected trailing newline, got: %q", buf.String())
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("invalid json: %v - %s", err, buf.String())
	}
	if decoded["msg"] != "hello" || decoded["trace_id"] != "trace-1" {
		t.Fatalf("unexpected entry: %+v", decoded)
	}
}

func TestMultiSink_FailureIsolated(t *testing.T) {
	good := newCountingSink()
	bad := &errorSink{}
	multi := NewMultiSink(bad, good)
	if err := multi.Write(LogEntry{Msg: "x"}); err == nil {
		t.Fatal("expected error from bad sink to surface")
	}
	if good.count.Load() != 1 {
		t.Fatalf("good sink not invoked: %d", good.count.Load())
	}
}

func TestUDPLogSink_RoundTrip(t *testing.T) {
	addr, srv, ch := startUDPServer(t)
	defer srv.Close()

	sink, err := NewUDPLogSink(addr)
	if err != nil {
		t.Fatalf("create udp sink: %v", err)
	}
	defer sink.Close()

	if err := sink.Write(LogEntry{Time: time.Now(), Level: LevelWarn, Msg: "udp"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case data := <-ch:
		if !bytes.Contains(data, []byte("\"udp\"")) {
			t.Fatalf("payload missing msg: %s", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("udp packet not received in time")
	}
}

func TestContextLogger_AppendsContextFields(t *testing.T) {
	rb := NewRingBufferSink(8)
	cl := NewContextLogger("node-A", rb)
	cl = cl.WithTrace("trace-X").WithActor("/user/match/1")

	cl.Info("matched", "elo", 1500)

	entries := rb.Snapshot()
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.NodeID != "node-A" || e.TraceID != "trace-X" || e.Actor != "/user/match/1" {
		t.Fatalf("context not propagated: %+v", e)
	}
	if e.Fields["elo"] != 1500 {
		t.Fatalf("missing field: %+v", e.Fields)
	}
}

func TestContextLogger_LevelFiltering(t *testing.T) {
	prev := GetLevel()
	SetLevel(LevelWarn)
	defer SetLevel(prev)

	rb := NewRingBufferSink(8)
	cl := NewContextLogger("n", rb)

	cl.Debug("ignored")
	cl.Info("ignored")
	cl.Warn("kept")
	cl.Error("kept")

	if got := rb.Len(); got != 2 {
		t.Fatalf("expected 2 entries after level filter, got %d", got)
	}
}

// ---- helpers ----

type countingSink struct {
	count atomic.Int64
}

func newCountingSink() *countingSink { return &countingSink{} }

func (s *countingSink) Write(LogEntry) error { s.count.Add(1); return nil }
func (s *countingSink) Flush() error         { return nil }
func (s *countingSink) Close() error         { return nil }

type errorSink struct{}

func (e *errorSink) Write(LogEntry) error { return errSinkFail }
func (e *errorSink) Flush() error         { return nil }
func (e *errorSink) Close() error         { return nil }

var errSinkFail = &fatalErr{"sink failed"}

type fatalErr struct{ s string }

func (e *fatalErr) Error() string { return e.s }

func startUDPServer(t *testing.T) (string, *net.UDPConn, chan []byte) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan []byte, 4)
	go func() {
		buf := make([]byte, 4096)
		for {
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			data := make([]byte, n)
			copy(data, buf[:n])
			ch <- data
		}
	}()
	return conn.LocalAddr().String(), conn, ch
}
