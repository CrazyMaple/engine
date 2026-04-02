package network

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"engine/log"
)

// WebsocketConnSet WebSocket 连接集合
type WebsocketConnSet map[*websocket.Conn]struct{}

// WSConn WebSocket 连接，实现 Conn 接口
type WSConn struct {
	sync.Mutex
	conn      *websocket.Conn
	writeChan chan []byte
	maxMsgLen uint32
	closeFlag bool
	pingStop  chan struct{} // Ping 心跳停止信号
}

func newWSConn(conn *websocket.Conn, pendingWriteNum int, maxMsgLen uint32) *WSConn {
	wsConn := &WSConn{
		conn:      conn,
		writeChan: make(chan []byte, pendingWriteNum),
		maxMsgLen: maxMsgLen,
	}

	go func() {
		for b := range wsConn.writeChan {
			if b == nil {
				break
			}

			err := conn.WriteMessage(websocket.BinaryMessage, b)
			if err != nil {
				break
			}
		}

		conn.Close()
		wsConn.Lock()
		wsConn.closeFlag = true
		wsConn.Unlock()
	}()

	return wsConn
}

func (wsConn *WSConn) doDestroy() {
	if c, ok := wsConn.conn.UnderlyingConn().(*net.TCPConn); ok {
		c.SetLinger(0)
	}
	wsConn.conn.Close()

	if !wsConn.closeFlag {
		close(wsConn.writeChan)
		wsConn.closeFlag = true
	}
	if wsConn.pingStop != nil {
		select {
		case <-wsConn.pingStop:
		default:
			close(wsConn.pingStop)
		}
	}
}

// Destroy 销毁连接
func (wsConn *WSConn) Destroy() {
	wsConn.Lock()
	defer wsConn.Unlock()
	wsConn.doDestroy()
}

// Close 关闭连接
func (wsConn *WSConn) Close() {
	wsConn.Lock()
	defer wsConn.Unlock()
	if wsConn.closeFlag {
		return
	}

	wsConn.doWrite(nil)
	wsConn.closeFlag = true
}

func (wsConn *WSConn) doWrite(b []byte) {
	if len(wsConn.writeChan) == cap(wsConn.writeChan) {
		log.Debug("close ws conn: channel full")
		wsConn.doDestroy()
		return
	}

	wsConn.writeChan <- b
}

// LocalAddr 本地地址
func (wsConn *WSConn) LocalAddr() net.Addr {
	return wsConn.conn.LocalAddr()
}

// RemoteAddr 远程地址
func (wsConn *WSConn) RemoteAddr() net.Addr {
	return wsConn.conn.RemoteAddr()
}

// ReadMsg 读取消息（WebSocket 自带分帧，无需 MsgParser）
func (wsConn *WSConn) ReadMsg() ([]byte, error) {
	_, b, err := wsConn.conn.ReadMessage()
	return b, err
}

// WriteMsg 写入消息
func (wsConn *WSConn) WriteMsg(args ...[]byte) error {
	wsConn.Lock()
	defer wsConn.Unlock()
	if wsConn.closeFlag {
		return nil
	}

	var msgLen uint32
	for i := 0; i < len(args); i++ {
		msgLen += uint32(len(args[i]))
	}

	if msgLen > wsConn.maxMsgLen {
		return errors.New("message too long")
	}
	if msgLen < 1 {
		return errors.New("message too short")
	}

	// 单段数据直接发送
	if len(args) == 1 {
		wsConn.doWrite(args[0])
		return nil
	}

	// 多段数据合并
	msg := make([]byte, msgLen)
	l := 0
	for i := 0; i < len(args); i++ {
		copy(msg[l:], args[i])
		l += len(args[i])
	}

	wsConn.doWrite(msg)
	return nil
}

// startPing 启动 Ping 心跳发送循环
func (wsConn *WSConn) startPing(interval time.Duration) {
	wsConn.pingStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				wsConn.Lock()
				closed := wsConn.closeFlag
				wsConn.Unlock()
				if closed {
					return
				}
				if err := wsConn.conn.WriteControl(
					websocket.PingMessage, nil, time.Now().Add(5*time.Second),
				); err != nil {
					return
				}
			case <-wsConn.pingStop:
				return
			}
		}
	}()
}
