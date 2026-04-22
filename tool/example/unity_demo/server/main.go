// Unity Demo 配套服务器
//
// 应用层协议：JSON 消息（每条含 "type" 字段）
//
//   - TCP（默认 :8000）：4 字节大端长度前缀 + JSON 载荷（与 C# TcpTransport 对齐）
//   - KCP（默认 :9100）：使用 engine/network KCPServer，按 KCP 原生消息边界传输 JSON 载荷（与 kcp2k 客户端对齐）
//
// 业务：登录 / 位置同步 / 世界聊天 / 定时排行榜推送
//
// 启动：
//
//	cd example/unity_demo/server && go run main.go
//
// 对端参考：../Assets/Scripts/TypedGameClient.cs
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"engine/network"
)

// ============================================================
// 协议 & 连接抽象
// ============================================================

// sendFunc 抽象两种传输的写路径：TCP 写 4-byte 前缀帧，KCP 直接 WriteMsg
type sendFunc func(payload []byte) error

type session struct {
	id        uint64
	name      string
	transport string
	send      sendFunc
	x, y, z   float32
	score     int64
	writeMu   sync.Mutex
}

func (s *session) SendJSON(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.send(buf)
}

// ============================================================
// 全局房间状态
// ============================================================

type hub struct {
	mu       sync.RWMutex
	sessions map[uint64]*session
	nextID   uint64
}

func newHub() *hub {
	return &hub{sessions: make(map[uint64]*session)}
}

func (h *hub) add(s *session) {
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
}

func (h *hub) remove(id uint64) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}

func (h *hub) broadcast(msg map[string]any) {
	h.mu.RLock()
	snap := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		snap = append(snap, s)
	}
	h.mu.RUnlock()
	for _, s := range snap {
		_ = s.SendJSON(msg)
	}
}

func (h *hub) snapshotLeaderboard(top int) []map[string]any {
	h.mu.RLock()
	list := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		list = append(list, s)
	}
	h.mu.RUnlock()
	sort.Slice(list, func(i, j int) bool { return list[i].score > list[j].score })
	if len(list) > top {
		list = list[:top]
	}
	out := make([]map[string]any, 0, len(list))
	for i, s := range list {
		out = append(out, map[string]any{"rank": i + 1, "player_id": s.name, "score": s.score})
	}
	return out
}

// ============================================================
// 业务处理
// ============================================================

func handleMsg(h *hub, s *session, env map[string]any) {
	t, _ := env["type"].(string)
	switch t {
	case "__ping__":
		return

	case "LoginRequest":
		s.name, _ = env["player_name"].(string)
		if s.name == "" {
			s.name = fmt.Sprintf("player-%d", s.id)
		}
		resp := map[string]any{
			"type":      "LoginResponse",
			"ok":        true,
			"player_id": s.name,
			"msg":       "welcome",
		}
		if v, ok := env["__rpc_id"]; ok {
			resp["__rpc_id"] = v
		}
		_ = s.SendJSON(resp)

	case "MoveRequest":
		s.x = toFloat(env["x"])
		s.y = toFloat(env["y"])
		s.z = toFloat(env["z"])
		atomic.AddInt64(&s.score, 1) // 移动一次 +1 分，仅为演示排行榜动态
		h.broadcast(map[string]any{
			"type": "PositionBroadcast", "player_id": s.name,
			"x": s.x, "y": s.y, "z": s.z,
		})

	case "ChatSendRequest":
		ch, _ := env["channel"].(string)
		if ch == "" {
			ch = "world"
		}
		content, _ := env["content"].(string)
		h.broadcast(map[string]any{
			"type": "ChatBroadcast", "channel": ch,
			"from": s.name, "content": content,
			"timestamp": time.Now().Unix(),
		})
	}
}

func toFloat(v any) float32 {
	switch n := v.(type) {
	case float64:
		return float32(n)
	case float32:
		return n
	case int:
		return float32(n)
	}
	return 0
}

// ============================================================
// 传输层：TCP
// ============================================================

func tcpSend(w io.Writer) sendFunc {
	return func(payload []byte) error {
		frame := make([]byte, 4+len(payload))
		binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
		copy(frame[4:], payload)
		_, err := w.Write(frame)
		return err
	}
}

func serveTCP(h *hub, conn net.Conn) {
	defer conn.Close()
	s := &session{
		id:        atomic.AddUint64(&h.nextID, 1),
		transport: "tcp",
		send:      tcpSend(conn),
	}
	h.add(s)
	defer h.remove(s.id)
	log.Printf("[tcp] connected id=%d addr=%s", s.id, conn.RemoteAddr())

	lenBuf := make([]byte, 4)
	for {
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			if err != io.EOF {
				log.Printf("[tcp] read len: %v", err)
			}
			return
		}
		n := binary.BigEndian.Uint32(lenBuf)
		if n == 0 || n > 64*1024 {
			log.Printf("[tcp] bad frame len=%d", n)
			return
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(conn, data); err != nil {
			return
		}
		var env map[string]any
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		handleMsg(h, s, env)
	}
}

func runTCP(h *hub, addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("tcp listen %s: %v", addr, err)
	}
	log.Printf("[tcp] listening on %s", addr)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("[tcp] accept: %v", err)
			continue
		}
		go serveTCP(h, c)
	}
}

// ============================================================
// 传输层：KCP（按 KCP 原生消息边界，无需 4 字节长度前缀）
// ============================================================

type kcpAgent struct {
	hub  *hub
	conn *network.KCPConn
	sess *session
}

func (a *kcpAgent) Run() {
	a.sess = &session{
		id:        atomic.AddUint64(&a.hub.nextID, 1),
		transport: "kcp",
		send:      func(p []byte) error { return a.conn.WriteMsg(p) },
	}
	a.hub.add(a.sess)
	log.Printf("[kcp] connected id=%d addr=%s", a.sess.id, a.conn.RemoteAddr())
	for {
		data, err := a.conn.ReadMsg()
		if err != nil {
			return
		}
		var env map[string]any
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		handleMsg(a.hub, a.sess, env)
	}
}

func (a *kcpAgent) OnClose() {
	if a.sess != nil {
		a.hub.remove(a.sess.id)
	}
}

func runKCP(h *hub, addr string) {
	srv := &network.KCPServer{
		Addr:       addr,
		MaxConnNum: 1024,
		Config:     network.FastKCPConfig(),
		NewAgent: func(c *network.KCPConn) network.Agent {
			return &kcpAgent{hub: h, conn: c}
		},
	}
	if err := srv.Start(); err != nil {
		log.Printf("[kcp] start %s: %v (跳过 KCP 接入)", addr, err)
		return
	}
	log.Printf("[kcp] listening on %s", addr)
}

// ============================================================
// 排行榜定时广播
// ============================================================

func leaderboardLoop(h *hub) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		entries := h.snapshotLeaderboard(10)
		if len(entries) == 0 {
			continue
		}
		h.broadcast(map[string]any{
			"type":    "LeaderboardNotify",
			"entries": entries,
		})
	}
}

func main() {
	tcpAddr := flag.String("tcp", "0.0.0.0:8000", "TCP 监听地址")
	kcpAddr := flag.String("kcp", "0.0.0.0:9100", "KCP 监听地址（UDP）")
	flag.Parse()

	h := newHub()
	go runTCP(h, *tcpAddr)
	runKCP(h, *kcpAddr)
	go leaderboardLoop(h)

	log.Printf("Unity demo server started. TCP=%s KCP=%s", *tcpAddr, *kcpAddr)
	select {}
}
