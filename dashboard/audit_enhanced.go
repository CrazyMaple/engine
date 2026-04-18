package dashboard

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ChainedAuditEntry 带哈希链的审计条目。
//
// 哈希链保证一旦写入，任何中间条目被篡改或删除都能在后续 Verify() 时被检测到：
//   Hash_n = SHA256(Hash_{n-1} || Seq || Time || Action || Detail || Source || Operator || SourceIP)
type ChainedAuditEntry struct {
	Seq      uint64    `json:"seq"`
	Time     time.Time `json:"time"`
	Action   string    `json:"action"`
	Detail   string    `json:"detail"`
	Source   string    `json:"source"`
	Operator string    `json:"operator"`
	SourceIP string    `json:"source_ip"`
	PrevHash string    `json:"prev_hash"` // hex，创世条目为空字符串
	Hash     string    `json:"hash"`       // hex，本条的校验值
}

// ChainedAuditLog 追加写、不可篡改的审计日志。
// 与 AuditLog（环形缓冲）不同，此结构全量保留日志，供合规审计与导出。
type ChainedAuditLog struct {
	mu       sync.RWMutex
	entries  []ChainedAuditEntry
	lastHash string
	nextSeq  uint64
}

// NewChainedAuditLog 创建空的哈希链审计日志。
func NewChainedAuditLog() *ChainedAuditLog {
	return &ChainedAuditLog{}
}

// Append 追加一条审计日志，并计算新的哈希。返回追加后的条目。
func (c *ChainedAuditLog) Append(action, detail, source, operator, sourceIP string) ChainedAuditEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := ChainedAuditEntry{
		Seq:      c.nextSeq,
		Time:     time.Now(),
		Action:   action,
		Detail:   detail,
		Source:   source,
		Operator: operator,
		SourceIP: sourceIP,
		PrevHash: c.lastHash,
	}
	entry.Hash = computeAuditHash(&entry)

	c.entries = append(c.entries, entry)
	c.lastHash = entry.Hash
	c.nextSeq++
	return entry
}

// Len 返回当前条目总数。
func (c *ChainedAuditLog) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// LastHash 返回链尾哈希（用于外部快照校验）。
func (c *ChainedAuditLog) LastHash() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastHash
}

// Verify 校验整个哈希链完整性。
// 返回 (valid, invalidSeq)；valid=true 时 invalidSeq 无意义；
// valid=false 时 invalidSeq 为首个不匹配的条目序号。
func (c *ChainedAuditLog) Verify() (bool, uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var prev string
	for i := range c.entries {
		e := c.entries[i]
		if e.PrevHash != prev {
			return false, e.Seq
		}
		if e.Hash != computeAuditHash(&e) {
			return false, e.Seq
		}
		prev = e.Hash
	}
	return true, 0
}

// AuditFilter 导出/查询条件，空字段表示不做过滤。
type AuditFilter struct {
	Operator string
	Action   string
	Source   string
	Since    time.Time
	Until    time.Time
}

func (f AuditFilter) match(e *ChainedAuditEntry) bool {
	if f.Operator != "" && f.Operator != e.Operator {
		return false
	}
	if f.Action != "" && f.Action != e.Action {
		return false
	}
	if f.Source != "" && f.Source != e.Source {
		return false
	}
	if !f.Since.IsZero() && e.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.Time.After(f.Until) {
		return false
	}
	return true
}

// Query 按过滤条件返回条目拷贝（不影响内部链）。
func (c *ChainedAuditLog) Query(f AuditFilter) []ChainedAuditEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]ChainedAuditEntry, 0, len(c.entries))
	for i := range c.entries {
		if f.match(&c.entries[i]) {
			out = append(out, c.entries[i])
		}
	}
	return out
}

// ExportJSON 导出符合条件的条目为 JSON 流（数组）。
func (c *ChainedAuditLog) ExportJSON(f AuditFilter, w io.Writer) error {
	entries := c.Query(f)
	enc := json.NewEncoder(w)
	return enc.Encode(entries)
}

// ExportCSV 导出符合条件的条目为 CSV。列顺序：seq,time,action,detail,source,operator,source_ip,prev_hash,hash。
func (c *ChainedAuditLog) ExportCSV(f AuditFilter, w io.Writer) error {
	entries := c.Query(f)
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write([]string{"seq", "time", "action", "detail", "source", "operator", "source_ip", "prev_hash", "hash"}); err != nil {
		return err
	}
	for _, e := range entries {
		row := []string{
			fmt.Sprintf("%d", e.Seq),
			e.Time.UTC().Format(time.RFC3339Nano),
			e.Action,
			e.Detail,
			e.Source,
			e.Operator,
			e.SourceIP,
			e.PrevHash,
			e.Hash,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	return nil
}

// computeAuditHash 计算单条审计的哈希值。
// 字段之间用 0x00 分隔，避免拼接歧义（"ab","c" 与 "a","bc" 不同）。
func computeAuditHash(e *ChainedAuditEntry) string {
	h := sha256.New()
	h.Write([]byte(e.PrevHash))
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], e.Seq)
	h.Write(buf[:])
	binary.BigEndian.PutUint64(buf[:], uint64(e.Time.UnixNano()))
	h.Write(buf[:])
	writeField(h, e.Action)
	writeField(h, e.Detail)
	writeField(h, e.Source)
	writeField(h, e.Operator)
	writeField(h, e.SourceIP)
	return hex.EncodeToString(h.Sum(nil))
}

func writeField(h io.Writer, s string) {
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(s))
}

// ---- 二次确认码 ----

// pendingConfirmation 等待二次确认的高危操作。
type pendingConfirmation struct {
	Action    string
	Detail    string
	Operator  string
	ExpiresAt time.Time
}

// ConfirmationManager 高危 GM 操作二次确认码管理器。
//
// 使用流程：
//  1. 前端请求 Request(action, detail, operator) 获取一次性确认码
//  2. 将确认码返回给操作者
//  3. 操作者携带确认码再次提交同一操作
//  4. 服务端 Verify(code, action, operator) 校验后执行（确认码立即失效）
type ConfirmationManager struct {
	mu      sync.Mutex
	pending map[string]*pendingConfirmation
	ttl     time.Duration
}

// NewConfirmationManager 创建管理器。ttl 为确认码有效期，<=0 时默认 2 分钟。
func NewConfirmationManager(ttl time.Duration) *ConfirmationManager {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &ConfirmationManager{
		pending: make(map[string]*pendingConfirmation),
		ttl:     ttl,
	}
}

// Request 为一次高危操作生成确认码。返回十六进制编码的 32 字符码。
func (m *ConfirmationManager) Request(action, detail, operator string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := hex.EncodeToString(raw)

	m.mu.Lock()
	m.pending[code] = &pendingConfirmation{
		Action:    action,
		Detail:    detail,
		Operator:  operator,
		ExpiresAt: time.Now().Add(m.ttl),
	}
	m.mu.Unlock()
	return code, nil
}

// Verify 校验确认码。成功返回原始 (action, detail) 并立即销毁该码。
// 失败返回 error；error 文本区分 "expired" / "not found" / "mismatch"。
// 校验要求：action 与 operator 与请求时完全一致（防止跨操作劫持）。
func (m *ConfirmationManager) Verify(code, action, operator string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.pending[code]
	if !ok {
		return "", errors.New("confirmation code not found")
	}
	// 无论成功失败，使用过一次就销毁，避免枚举
	delete(m.pending, code)

	if time.Now().After(p.ExpiresAt) {
		return "", errors.New("confirmation code expired")
	}
	if p.Action != action || p.Operator != operator {
		return "", errors.New("confirmation code action/operator mismatch")
	}
	return p.Detail, nil
}

// Cleanup 清理过期的确认码。可由定时器定期调用。
func (m *ConfirmationManager) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	removed := 0
	for code, p := range m.pending {
		if now.After(p.ExpiresAt) {
			delete(m.pending, code)
			removed++
		}
	}
	return removed
}

// Pending 返回等待确认的操作数（用于监控）。
func (m *ConfirmationManager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// ---- HTTP 处理器 ----

// AuditEnhancedHandlers 合规审计增强 API 处理器。
type AuditEnhancedHandlers struct {
	audit   *ChainedAuditLog
	confirm *ConfirmationManager
}

// NewAuditEnhancedHandlers 创建处理器。confirm 可为 nil（仅启用审计导出/校验）。
func NewAuditEnhancedHandlers(audit *ChainedAuditLog, confirm *ConfirmationManager) *AuditEnhancedHandlers {
	return &AuditEnhancedHandlers{audit: audit, confirm: confirm}
}

// RegisterRoutes 将审计增强路由注册到 mux。
func (h *AuditEnhancedHandlers) RegisterRoutes(mux *http.ServeMux) {
	if h.audit != nil {
		mux.HandleFunc("/api/audit/chained/query", h.handleQuery)
		mux.HandleFunc("/api/audit/chained/export", h.handleExport)
		mux.HandleFunc("/api/audit/chained/verify", h.handleVerify)
	}
	if h.confirm != nil {
		mux.HandleFunc("/api/audit/confirm/request", h.handleConfirmRequest)
		mux.HandleFunc("/api/audit/confirm/submit", h.handleConfirmSubmit)
	}
}

func (h *AuditEnhancedHandlers) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	entries := h.audit.Query(filterFromQuery(r))
	writeJSON(w, entries)
}

func (h *AuditEnhancedHandlers) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	f := filterFromQuery(r)
	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=audit.csv")
		if err := h.audit.ExportCSV(f, w); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := h.audit.ExportJSON(f, w); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
	}
}

func (h *AuditEnhancedHandlers) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	valid, seq := h.audit.Verify()
	writeJSON(w, map[string]interface{}{
		"valid":         valid,
		"invalid_seq":   seq,
		"total_entries": h.audit.Len(),
		"last_hash":     h.audit.LastHash(),
	})
}

func (h *AuditEnhancedHandlers) handleConfirmRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Action   string `json:"action"`
		Detail   string `json:"detail"`
		Operator string `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action == "" || req.Operator == "" {
		writeError(w, http.StatusBadRequest, "action and operator are required")
		return
	}
	code, err := h.confirm.Request(req.Action, req.Detail, req.Operator)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]string{"code": code})
}

func (h *AuditEnhancedHandlers) handleConfirmSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Code     string `json:"code"`
		Action   string `json:"action"`
		Operator string `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	detail, err := h.confirm.Verify(req.Code, req.Action, req.Operator)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "confirmed", "detail": detail})
}

func filterFromQuery(r *http.Request) AuditFilter {
	q := r.URL.Query()
	f := AuditFilter{
		Operator: q.Get("operator"),
		Action:   q.Get("action"),
		Source:   q.Get("source"),
	}
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.Since = t
		}
	}
	if s := q.Get("until"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.Until = t
		}
	}
	return f
}
