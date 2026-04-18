package log

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// LogEntry 单条结构化日志
type LogEntry struct {
	Time    time.Time              `json:"time"`
	Level   Level                  `json:"level"`
	Msg     string                 `json:"msg"`
	NodeID  string                 `json:"node_id,omitempty"`
	TraceID string                 `json:"trace_id,omitempty"`
	Actor   string                 `json:"actor,omitempty"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
}

// LogSink 日志输出后端接口
type LogSink interface {
	Write(entry LogEntry) error
	Flush() error
	Close() error
}

// ContextLogger 自动附加节点 ID/TraceID/ActorPath 的 Logger 包装器
// 通过 Sink 链路输出，每条日志携带可观测上下文
type ContextLogger struct {
	sinks   []LogSink
	nodeID  string
	traceID string
	actor   string
	fields  []interface{}
	level   Level
	mu      sync.RWMutex
}

// NewContextLogger 创建新的上下文 Logger
func NewContextLogger(nodeID string, sinks ...LogSink) *ContextLogger {
	return &ContextLogger{
		sinks:  sinks,
		nodeID: nodeID,
		level:  GetLevel(),
	}
}

// WithTrace 返回带 TraceID 的子 Logger
func (cl *ContextLogger) WithTrace(traceID string) *ContextLogger {
	c := cl.copy()
	c.traceID = traceID
	return c
}

// WithActor 返回带 Actor 路径的子 Logger
func (cl *ContextLogger) WithActor(actorPath string) *ContextLogger {
	c := cl.copy()
	c.actor = actorPath
	return c
}

// WithNode 返回带节点 ID 的子 Logger
func (cl *ContextLogger) WithNode(nodeID string) *ContextLogger {
	c := cl.copy()
	c.nodeID = nodeID
	return c
}

// With 返回带预设字段的 Logger
func (cl *ContextLogger) With(kvs ...interface{}) Logger {
	c := cl.copy()
	c.fields = append(c.fields, kvs...)
	return c
}

func (cl *ContextLogger) copy() *ContextLogger {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	dup := &ContextLogger{
		sinks:   cl.sinks,
		nodeID:  cl.nodeID,
		traceID: cl.traceID,
		actor:   cl.actor,
		level:   cl.level,
	}
	if len(cl.fields) > 0 {
		dup.fields = make([]interface{}, len(cl.fields))
		copy(dup.fields, cl.fields)
	}
	return dup
}

// AddSink 追加一个 Sink
func (cl *ContextLogger) AddSink(s LogSink) {
	cl.mu.Lock()
	cl.sinks = append(cl.sinks, s)
	cl.mu.Unlock()
}

// Debug 调试日志
func (cl *ContextLogger) Debug(msg string, kvs ...interface{}) {
	cl.emit(LevelDebug, msg, kvs)
}

// Info 信息日志
func (cl *ContextLogger) Info(msg string, kvs ...interface{}) {
	cl.emit(LevelInfo, msg, kvs)
}

// Warn 警告日志
func (cl *ContextLogger) Warn(msg string, kvs ...interface{}) {
	cl.emit(LevelWarn, msg, kvs)
}

// Error 错误日志
func (cl *ContextLogger) Error(msg string, kvs ...interface{}) {
	cl.emit(LevelError, msg, kvs)
}

func (cl *ContextLogger) emit(level Level, msg string, kvs []interface{}) {
	if !Enabled(level) {
		return
	}
	entry := LogEntry{
		Time:    time.Now(),
		Level:   level,
		Msg:     msg,
		NodeID:  cl.nodeID,
		TraceID: cl.traceID,
		Actor:   cl.actor,
	}
	if len(cl.fields) > 0 || len(kvs) > 0 {
		entry.Fields = make(map[string]interface{}, (len(cl.fields)+len(kvs))/2)
		mergeKVs(entry.Fields, cl.fields)
		mergeKVs(entry.Fields, kvs)
	}

	cl.mu.RLock()
	sinks := cl.sinks
	cl.mu.RUnlock()
	for _, s := range sinks {
		_ = s.Write(entry)
	}
}

func mergeKVs(m map[string]interface{}, kvs []interface{}) {
	for i := 0; i+1 < len(kvs); i += 2 {
		m[fmt.Sprint(kvs[i])] = kvs[i+1]
	}
}

// FormatEntry 将 LogEntry 序列化为单行 JSON
func FormatEntry(entry LogEntry) ([]byte, error) {
	type jsonEntry struct {
		Time    string                 `json:"time"`
		Level   string                 `json:"level"`
		Msg     string                 `json:"msg"`
		NodeID  string                 `json:"node_id,omitempty"`
		TraceID string                 `json:"trace_id,omitempty"`
		Actor   string                 `json:"actor,omitempty"`
		Fields  map[string]interface{} `json:"fields,omitempty"`
	}
	je := jsonEntry{
		Time:    entry.Time.Format(time.RFC3339Nano),
		Level:   entry.Level.String(),
		Msg:     entry.Msg,
		NodeID:  entry.NodeID,
		TraceID: entry.TraceID,
		Actor:   entry.Actor,
		Fields:  entry.Fields,
	}
	data, err := json.Marshal(je)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// ===== FileLogSink =====

// FileLogSink 本地文件 Sink，将每条 LogEntry 序列化为 JSON 行并写入文件
type FileLogSink struct {
	w   io.Writer
	mu  sync.Mutex
	own io.Closer // 持有需要关闭的资源（如 *os.File）
}

// NewFileLogSink 以追加模式打开（或创建）文件作为日志后端
func NewFileLogSink(path string) (*FileLogSink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &FileLogSink{w: f, own: f}, nil
}

// NewWriterSink 用任意 io.Writer 构建 Sink（测试常用）
func NewWriterSink(w io.Writer) *FileLogSink {
	return &FileLogSink{w: w}
}

func (s *FileLogSink) Write(entry LogEntry) error {
	data, err := FormatEntry(entry)
	if err != nil {
		return err
	}
	s.mu.Lock()
	_, err = s.w.Write(data)
	s.mu.Unlock()
	return err
}

func (s *FileLogSink) Flush() error {
	if f, ok := s.w.(interface{ Sync() error }); ok {
		return f.Sync()
	}
	return nil
}

func (s *FileLogSink) Close() error {
	if s.own != nil {
		return s.own.Close()
	}
	return nil
}

// ===== UDPLogSink =====

// UDPLogSink 通过 UDP 发送日志至集中收集端（高性能，可丢失）
type UDPLogSink struct {
	conn    *net.UDPConn
	addr    *net.UDPAddr
	dropped atomic.Int64
}

// NewUDPLogSink 连接到 udp://addr，建立写入通道
func NewUDPLogSink(addr string) (*UDPLogSink, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, err
	}
	return &UDPLogSink{conn: conn, addr: udpAddr}, nil
}

func (s *UDPLogSink) Write(entry LogEntry) error {
	data, err := FormatEntry(entry)
	if err != nil {
		return err
	}
	if _, err := s.conn.Write(data); err != nil {
		s.dropped.Add(1)
		return err
	}
	return nil
}

func (s *UDPLogSink) Flush() error { return nil }

func (s *UDPLogSink) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// Dropped 已丢弃日志数（写失败计数）
func (s *UDPLogSink) Dropped() int64 { return s.dropped.Load() }

// ===== TCPLogSink =====

// TCPLogSink 通过 TCP 流转发日志，相对 UDP 可靠但慢
// 自动断线重连（Write 时检测）
type TCPLogSink struct {
	addr  string
	conn  net.Conn
	mu    sync.Mutex
	queue chan []byte
	stop  chan struct{}
	wg    sync.WaitGroup
}

// NewTCPLogSink 异步发送，内部带缓冲队列；queueSize 推荐 >=256
func NewTCPLogSink(addr string, queueSize int) (*TCPLogSink, error) {
	if queueSize <= 0 {
		queueSize = 256
	}
	s := &TCPLogSink{
		addr:  addr,
		queue: make(chan []byte, queueSize),
		stop:  make(chan struct{}),
	}
	if err := s.dial(); err != nil {
		return nil, err
	}
	s.wg.Add(1)
	go s.loop()
	return s, nil
}

func (s *TCPLogSink) dial() error {
	conn, err := net.DialTimeout("tcp", s.addr, 3*time.Second)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn = conn
	s.mu.Unlock()
	return nil
}

func (s *TCPLogSink) loop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case data := <-s.queue:
			if err := s.send(data); err != nil {
				time.Sleep(500 * time.Millisecond)
				_ = s.dial()
				_ = s.send(data)
			}
		}
	}
}

func (s *TCPLogSink) send(data []byte) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return errors.New("no tcp connection")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Write(data)
	return err
}

func (s *TCPLogSink) Write(entry LogEntry) error {
	data, err := FormatEntry(entry)
	if err != nil {
		return err
	}
	select {
	case s.queue <- data:
		return nil
	default:
		return errors.New("tcp log queue full")
	}
}

func (s *TCPLogSink) Flush() error { return nil }

func (s *TCPLogSink) Close() error {
	close(s.stop)
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}
	return nil
}

// ===== MultiSink =====

// MultiSink 将一条日志同时写入多个 Sink，单个 Sink 失败不影响其他
type MultiSink struct {
	sinks []LogSink
	mu    sync.RWMutex
}

// NewMultiSink 创建复合 Sink
func NewMultiSink(sinks ...LogSink) *MultiSink {
	return &MultiSink{sinks: sinks}
}

// Add 追加 Sink
func (m *MultiSink) Add(s LogSink) {
	m.mu.Lock()
	m.sinks = append(m.sinks, s)
	m.mu.Unlock()
}

func (m *MultiSink) Write(entry LogEntry) error {
	m.mu.RLock()
	sinks := m.sinks
	m.mu.RUnlock()
	var firstErr error
	for _, s := range sinks {
		if err := s.Write(entry); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *MultiSink) Flush() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var firstErr error
	for _, s := range m.sinks {
		if err := s.Flush(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *MultiSink) Close() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var firstErr error
	for _, s := range m.sinks {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
