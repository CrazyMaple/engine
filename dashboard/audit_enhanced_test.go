package dashboard

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChainedAuditLog_AppendBuildsHashChain(t *testing.T) {
	al := NewChainedAuditLog()
	e1 := al.Append("kick", "player=1", "gm", "alice", "127.0.0.1")
	e2 := al.Append("ban", "player=2", "gm", "alice", "127.0.0.1")
	e3 := al.Append("mail", "broadcast", "gm", "bob", "127.0.0.1")

	if e1.Seq != 0 || e2.Seq != 1 || e3.Seq != 2 {
		t.Fatalf("sequence mismatch: %d/%d/%d", e1.Seq, e2.Seq, e3.Seq)
	}
	if e1.PrevHash != "" {
		t.Fatal("genesis entry prev_hash must be empty")
	}
	if e2.PrevHash != e1.Hash || e3.PrevHash != e2.Hash {
		t.Fatal("hash chain broken")
	}
	if al.LastHash() != e3.Hash {
		t.Fatal("LastHash() must equal last entry hash")
	}
}

func TestChainedAuditLog_VerifyValid(t *testing.T) {
	al := NewChainedAuditLog()
	for i := 0; i < 10; i++ {
		al.Append("act", "detail", "src", "op", "ip")
	}
	if ok, _ := al.Verify(); !ok {
		t.Fatal("verify should pass on pristine log")
	}
}

func TestChainedAuditLog_VerifyDetectsTamper(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("a1", "d1", "s", "o", "ip")
	al.Append("a2", "d2", "s", "o", "ip")
	al.Append("a3", "d3", "s", "o", "ip")

	// 篡改中间条目的 Detail 字段（绕过 Append，直接修改内部切片）
	al.mu.Lock()
	al.entries[1].Detail = "HACKED"
	al.mu.Unlock()

	ok, seq := al.Verify()
	if ok {
		t.Fatal("verify should detect tampered detail")
	}
	if seq != 1 {
		t.Errorf("expected invalid_seq=1, got %d", seq)
	}
}

func TestChainedAuditLog_VerifyDetectsDeletion(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("a1", "d1", "s", "o", "ip")
	al.Append("a2", "d2", "s", "o", "ip")
	al.Append("a3", "d3", "s", "o", "ip")

	// 删除中间条目
	al.mu.Lock()
	al.entries = append(al.entries[:1], al.entries[2:]...)
	al.mu.Unlock()

	ok, _ := al.Verify()
	if ok {
		t.Fatal("verify should detect deletion breaking the chain")
	}
}

func TestChainedAuditLog_EmptyVerify(t *testing.T) {
	al := NewChainedAuditLog()
	if ok, _ := al.Verify(); !ok {
		t.Fatal("empty log should verify as valid")
	}
}

func TestChainedAuditLog_QueryFilters(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("kick", "p1", "gm", "alice", "1.1.1.1")
	al.Append("ban", "p2", "gm", "alice", "1.1.1.1")
	al.Append("kick", "p3", "gm", "bob", "2.2.2.2")
	al.Append("mail", "p4", "api", "alice", "1.1.1.1")

	// 按 operator
	entries := al.Query(AuditFilter{Operator: "alice"})
	if len(entries) != 3 {
		t.Errorf("expected 3 alice entries, got %d", len(entries))
	}

	// 按 action
	entries = al.Query(AuditFilter{Action: "kick"})
	if len(entries) != 2 {
		t.Errorf("expected 2 kick entries, got %d", len(entries))
	}

	// 按 source
	entries = al.Query(AuditFilter{Source: "api"})
	if len(entries) != 1 {
		t.Errorf("expected 1 api entry, got %d", len(entries))
	}

	// 组合：alice 的 kick
	entries = al.Query(AuditFilter{Operator: "alice", Action: "kick"})
	if len(entries) != 1 {
		t.Errorf("expected 1 alice kick entry, got %d", len(entries))
	}
}

func TestChainedAuditLog_QueryTimeRange(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("a1", "d", "s", "o", "ip")
	middle := time.Now()
	time.Sleep(2 * time.Millisecond)
	al.Append("a2", "d", "s", "o", "ip")

	entries := al.Query(AuditFilter{Since: middle})
	if len(entries) != 1 || entries[0].Action != "a2" {
		t.Fatalf("since filter failed: %+v", entries)
	}

	entries = al.Query(AuditFilter{Until: middle})
	if len(entries) != 1 || entries[0].Action != "a1" {
		t.Fatalf("until filter failed: %+v", entries)
	}
}

func TestChainedAuditLog_ExportJSON(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("a1", "d1", "s", "alice", "ip")
	al.Append("a2", "d2", "s", "bob", "ip")

	var buf bytes.Buffer
	if err := al.ExportJSON(AuditFilter{}, &buf); err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var out []ChainedAuditEntry
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 2 || out[0].Action != "a1" || out[1].Action != "a2" {
		t.Fatalf("export mismatch: %+v", out)
	}
}

func TestChainedAuditLog_ExportCSV(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("kick", "reason=afk", "gm", "alice", "1.1.1.1")
	al.Append("ban", "reason=cheat", "gm", "alice", "1.1.1.1")

	var buf bytes.Buffer
	if err := al.ExportCSV(AuditFilter{Operator: "alice"}, &buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	r := csv.NewReader(&buf)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	// 1 header + 2 rows
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	header := rows[0]
	if header[0] != "seq" || header[2] != "action" || header[8] != "hash" {
		t.Fatalf("bad header: %v", header)
	}
	if rows[1][2] != "kick" || rows[2][2] != "ban" {
		t.Fatalf("row action mismatch: %+v", rows[1:])
	}
}

// ---- ConfirmationManager ----

func TestConfirmation_RequestSubmitRoundTrip(t *testing.T) {
	m := NewConfirmationManager(time.Minute)
	code, err := m.Request("delete_all", "tenant=prod", "alice")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(code) != 32 {
		t.Fatalf("expected 32-char hex code, got %q", code)
	}

	detail, err := m.Verify(code, "delete_all", "alice")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if detail != "tenant=prod" {
		t.Errorf("detail mismatch: %s", detail)
	}
}

func TestConfirmation_CodeSingleUse(t *testing.T) {
	m := NewConfirmationManager(time.Minute)
	code, _ := m.Request("x", "d", "op")
	if _, err := m.Verify(code, "x", "op"); err != nil {
		t.Fatalf("first verify failed: %v", err)
	}
	if _, err := m.Verify(code, "x", "op"); err == nil {
		t.Fatal("second verify should fail (single-use)")
	}
}

func TestConfirmation_Expired(t *testing.T) {
	m := NewConfirmationManager(10 * time.Millisecond)
	code, _ := m.Request("x", "d", "op")
	time.Sleep(30 * time.Millisecond)
	if _, err := m.Verify(code, "x", "op"); err == nil {
		t.Fatal("expected expiration error")
	}
}

func TestConfirmation_ActionMismatch(t *testing.T) {
	m := NewConfirmationManager(time.Minute)
	code, _ := m.Request("delete_all", "d", "alice")
	if _, err := m.Verify(code, "kick_one", "alice"); err == nil {
		t.Fatal("action mismatch should be rejected")
	}
}

func TestConfirmation_OperatorMismatch(t *testing.T) {
	m := NewConfirmationManager(time.Minute)
	code, _ := m.Request("x", "d", "alice")
	if _, err := m.Verify(code, "x", "bob"); err == nil {
		t.Fatal("operator mismatch should be rejected")
	}
}

func TestConfirmation_CodeConsumedEvenOnMismatch(t *testing.T) {
	m := NewConfirmationManager(time.Minute)
	code, _ := m.Request("x", "d", "alice")

	// 先用错误 action 消耗
	if _, err := m.Verify(code, "wrong", "alice"); err == nil {
		t.Fatal("mismatch should error")
	}
	// 即使正确 action 也不能再用（防暴力枚举）
	if _, err := m.Verify(code, "x", "alice"); err == nil {
		t.Fatal("code should be consumed after failed attempt")
	}
}

func TestConfirmation_Cleanup(t *testing.T) {
	m := NewConfirmationManager(10 * time.Millisecond)
	_, _ = m.Request("x", "d", "a")
	_, _ = m.Request("y", "d", "a")
	if m.Pending() != 2 {
		t.Fatalf("expected 2 pending, got %d", m.Pending())
	}
	time.Sleep(30 * time.Millisecond)
	removed := m.Cleanup()
	if removed != 2 {
		t.Errorf("expected 2 cleaned up, got %d", removed)
	}
	if m.Pending() != 0 {
		t.Errorf("expected 0 pending after cleanup, got %d", m.Pending())
	}
}

// ---- HTTP handlers ----

func TestAuditEnhancedHandlers_Query(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("kick", "d", "s", "alice", "ip")
	al.Append("ban", "d", "s", "bob", "ip")

	h := NewAuditEnhancedHandlers(al, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/audit/chained/query?operator=alice", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", rec.Code, rec.Body.String())
	}
	var entries []ChainedAuditEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 || entries[0].Operator != "alice" {
		t.Fatalf("filter failed: %+v", entries)
	}
}

func TestAuditEnhancedHandlers_ExportCSV(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("a1", "d", "s", "o", "ip")

	h := NewAuditEnhancedHandlers(al, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/audit/chained/export?format=csv", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("expected csv content type, got %s", ct)
	}
	if !strings.Contains(rec.Body.String(), "a1") {
		t.Errorf("csv missing row: %s", rec.Body.String())
	}
}

func TestAuditEnhancedHandlers_Verify(t *testing.T) {
	al := NewChainedAuditLog()
	al.Append("a", "d", "s", "o", "ip")
	al.Append("b", "d", "s", "o", "ip")

	h := NewAuditEnhancedHandlers(al, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/audit/chained/verify", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if v, _ := resp["valid"].(bool); !v {
		t.Fatalf("expected valid=true, got %+v", resp)
	}
}

func TestAuditEnhancedHandlers_ConfirmFlow(t *testing.T) {
	cm := NewConfirmationManager(time.Minute)
	h := NewAuditEnhancedHandlers(nil, cm)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 请求确认码
	body := bytes.NewBufferString(`{"action":"delete_all","detail":"prod","operator":"alice"}`)
	req := httptest.NewRequest("POST", "/api/audit/confirm/request", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("request status: %d, body: %s", rec.Code, rec.Body.String())
	}
	var r1 map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &r1)
	code := r1["code"]
	if code == "" {
		t.Fatal("empty code")
	}

	// 提交确认码
	body2 := bytes.NewBufferString(`{"code":"` + code + `","action":"delete_all","operator":"alice"}`)
	req2 := httptest.NewRequest("POST", "/api/audit/confirm/submit", body2)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("submit status: %d, body: %s", rec2.Code, rec2.Body.String())
	}
}

func TestAuditEnhancedHandlers_ConfirmRejectWrongOperator(t *testing.T) {
	cm := NewConfirmationManager(time.Minute)
	h := NewAuditEnhancedHandlers(nil, cm)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	code, _ := cm.Request("x", "d", "alice")
	body := bytes.NewBufferString(`{"code":"` + code + `","action":"x","operator":"mallory"}`)
	req := httptest.NewRequest("POST", "/api/audit/confirm/submit", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
