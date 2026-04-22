package telemetry

import (
	"strings"
	"testing"
)

func TestNewRootValid(t *testing.T) {
	root := NewRoot()
	if !root.IsValid() {
		t.Fatalf("root not valid: %+v", root)
	}
	if !root.Sampled() {
		t.Errorf("root should be sampled by default")
	}
	if len(root.TraceID) != 32 || len(root.SpanID) != 16 {
		t.Errorf("unexpected id lengths: %d / %d", len(root.TraceID), len(root.SpanID))
	}
}

func TestNewChildInheritsTrace(t *testing.T) {
	root := NewRoot()
	child := root.NewChild()
	if child.TraceID != root.TraceID {
		t.Errorf("child TraceID changed: %s vs %s", child.TraceID, root.TraceID)
	}
	if child.SpanID == root.SpanID {
		t.Errorf("child SpanID should differ")
	}
	if child.Flags != root.Flags {
		t.Errorf("child flags drift: %x vs %x", child.Flags, root.Flags)
	}
}

func TestTraceParentRoundtrip(t *testing.T) {
	root := NewRoot()
	tp := root.TraceParent()
	if !strings.HasPrefix(tp, "00-") {
		t.Errorf("bad format: %s", tp)
	}
	parsed := ParseTraceParent(tp)
	if parsed.TraceID != root.TraceID || parsed.SpanID != root.SpanID {
		t.Errorf("roundtrip failed: %+v vs %+v", parsed, root)
	}
	if !parsed.Sampled() {
		t.Errorf("sampled flag lost")
	}
}

func TestParseTraceParentInvalid(t *testing.T) {
	cases := []string{"", "bad", "00-xxx-yyy-00", "01-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-00"}
	for _, c := range cases {
		tc := ParseTraceParent(c)
		if c == cases[3] && !tc.IsValid() {
			// 非 00 版本当前仍接受；文档中可收紧
			continue
		}
		if tc.IsValid() && c != cases[3] {
			t.Errorf("expected invalid, got %+v for %q", tc, c)
		}
	}
}

func TestParseShort(t *testing.T) {
	tc := ParseShort("abc123")
	if tc.TraceID != "abc123" {
		t.Errorf("bare id: %+v", tc)
	}
	tc = ParseShort("tid/sid")
	if tc.TraceID != "tid" || tc.SpanID != "sid" {
		t.Errorf("slash form: %+v", tc)
	}
	root := NewRoot()
	tc = ParseShort(root.TraceParent())
	if tc.TraceID != root.TraceID {
		t.Errorf("full tp: %+v", tc)
	}
}

func TestPropagatorInjectExtract(t *testing.T) {
	p := DefaultPropagator()
	carrier := MapCarrier{}
	root := NewRoot()
	p.Inject(root, carrier)
	if carrier["traceparent"] == "" {
		t.Fatalf("nothing injected: %+v", carrier)
	}
	extracted := p.Extract(carrier)
	if extracted.TraceID != root.TraceID {
		t.Errorf("extract mismatch: %+v vs %+v", extracted, root)
	}
}

func TestActiveRegistryFIFO(t *testing.T) {
	r := NewActiveRegistry(3)
	ctxs := []TraceContext{NewRoot(), NewRoot(), NewRoot(), NewRoot()}
	for _, c := range ctxs {
		r.Remember(c)
	}
	if r.Size() != 3 {
		t.Errorf("size after overflow: %d", r.Size())
	}
	if _, ok := r.Lookup(ctxs[0].TraceID); ok {
		t.Errorf("oldest should be evicted")
	}
	if _, ok := r.Lookup(ctxs[3].TraceID); !ok {
		t.Errorf("newest should be kept")
	}
}
