package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"engine/log"
)

// logWSUpgrader 复用全局配置，允许跨域（Dashboard 内网部署）
var logWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// logWSPayload 推送给前端的单条日志（对齐 /api/log/query 返回结构）
type logWSPayload struct {
	Time    string                 `json:"time"`
	Level   string                 `json:"level"`
	Msg     string                 `json:"msg"`
	NodeID  string                 `json:"node_id,omitempty"`
	TraceID string                 `json:"trace_id,omitempty"`
	Actor   string                 `json:"actor,omitempty"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
}

// logWSSubscriber 每个 WebSocket 连接对应一个订阅者
//
// Notify 非阻塞：若 sendCh 满则丢弃该条消息并递增 dropped 计数（典型弱网 / 慢消费场景）。
// 连接关闭由 read goroutine 发现对端断开后触发，写循环通过 close(closeCh) 退出。
type logWSSubscriber struct {
	conn    *websocket.Conn
	sendCh  chan log.LogEntry
	closeCh chan struct{}
	once    sync.Once
	filter  log.QueryFilter
	dropped uint64
}

// Notify 符合 log.LogSubscriber 接口：非阻塞推送到 sendCh，满则丢弃
func (s *logWSSubscriber) Notify(entry log.LogEntry) {
	if !matchWSFilter(entry, s.filter) {
		return
	}
	select {
	case s.sendCh <- entry:
	default:
		s.dropped++
	}
}

// close 关闭连接和 closeCh（一次性）
func (s *logWSSubscriber) close() {
	s.once.Do(func() {
		close(s.closeCh)
		_ = s.conn.Close()
	})
}

// ---- GET /ws/log ----
//
// 查询参数（可选，与 /api/log/query 语义一致）：
//   trace_id / actor / node / level / msg
//
// 返回：WebSocket 流，每条消息为一条 logWSPayload JSON
func (h *handlers) handleLogWS(w http.ResponseWriter, r *http.Request) {
	bs := h.config.LogBroadcast
	if bs == nil {
		writeError(w, http.StatusServiceUnavailable, "log broadcast sink not configured")
		return
	}

	conn, err := logWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// 解析过滤条件（与 handleLogQuery 保持一致）
	q := r.URL.Query()
	filter := log.QueryFilter{
		TraceID:   q.Get("trace_id"),
		Actor:     q.Get("actor"),
		NodeID:    q.Get("node"),
		MsgSubstr: q.Get("msg"),
	}
	if lv := q.Get("level"); lv != "" {
		if parsed, err := log.ParseLevel(lv); err == nil {
			filter.MinLevel = parsed
		}
	}

	sub := &logWSSubscriber{
		conn:    conn,
		sendCh:  make(chan log.LogEntry, 64),
		closeCh: make(chan struct{}),
		filter:  filter,
	}
	cancel := bs.Subscribe(sub)

	// 读循环：主要检测客户端断开
	go func() {
		defer func() {
			cancel()
			sub.close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// 写循环（同步在当前 goroutine，简化 handler 生命周期）
	writeLogWSLoop(sub)
}

func writeLogWSLoop(sub *logWSSubscriber) {
	for {
		select {
		case <-sub.closeCh:
			return
		case entry := <-sub.sendCh:
			payload := logWSPayload{
				Time:    entry.Time.Format(time.RFC3339Nano),
				Level:   entry.Level.String(),
				Msg:     entry.Msg,
				NodeID:  entry.NodeID,
				TraceID: entry.TraceID,
				Actor:   entry.Actor,
				Fields:  entry.Fields,
			}
			data, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			_ = sub.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := sub.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}

// matchWSFilter 应用订阅者过滤条件
//
// 复用 /api/log/query 的 QueryFilter 语义，但时间下限/上限 / Limit 字段对实时流无意义
func matchWSFilter(e log.LogEntry, f log.QueryFilter) bool {
	if f.TraceID != "" && e.TraceID != f.TraceID {
		return false
	}
	if f.NodeID != "" && e.NodeID != f.NodeID {
		return false
	}
	if f.Actor != "" && !strings.Contains(e.Actor, f.Actor) {
		return false
	}
	if e.Level < f.MinLevel {
		return false
	}
	if f.MsgSubstr != "" && !strings.Contains(e.Msg, f.MsgSubstr) {
		return false
	}
	return true
}
