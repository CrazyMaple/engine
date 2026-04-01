package network

import (
	"net"
	"sync"
)

// TCPConn TCP连接
type TCPConn struct {
	sync.Mutex
	conn      net.Conn
	writeChan chan []byte
	closeFlag bool
	msgParser *MsgParser
}

// newTCPConn 创建TCP连接
func newTCPConn(conn net.Conn, pendingWriteNum int, msgParser *MsgParser) *TCPConn {
	tcpConn := &TCPConn{
		conn:      conn,
		writeChan: make(chan []byte, pendingWriteNum),
		msgParser: msgParser,
	}

	go func() {
		for b := range tcpConn.writeChan {
			if b == nil {
				break
			}

			_, err := conn.Write(b)
			if err != nil {
				break
			}
		}

		conn.Close()
		tcpConn.Lock()
		tcpConn.closeFlag = true
		tcpConn.Unlock()
	}()

	return tcpConn
}

func (tc *TCPConn) doDestroy() {
	tc.conn.(*net.TCPConn).SetLinger(0)
	tc.conn.Close()

	if !tc.closeFlag {
		close(tc.writeChan)
		tc.closeFlag = true
	}
}

func (tc *TCPConn) Destroy() {
	tc.Lock()
	defer tc.Unlock()
	tc.doDestroy()
}

func (tc *TCPConn) Close() {
	tc.Lock()
	defer tc.Unlock()
	if tc.closeFlag {
		return
	}

	tc.doWrite(nil)
	tc.closeFlag = true
}

func (tc *TCPConn) doWrite(b []byte) {
	if len(tc.writeChan) == cap(tc.writeChan) {
		tc.doDestroy()
		return
	}

	tc.writeChan <- b
}

func (tc *TCPConn) Write(b []byte) {
	tc.Lock()
	defer tc.Unlock()
	if tc.closeFlag || b == nil {
		return
	}

	tc.doWrite(b)
}

func (tc *TCPConn) Read(b []byte) (int, error) {
	return tc.conn.Read(b)
}

func (tc *TCPConn) LocalAddr() net.Addr {
	return tc.conn.LocalAddr()
}

func (tc *TCPConn) RemoteAddr() net.Addr {
	return tc.conn.RemoteAddr()
}

func (tc *TCPConn) ReadMsg() ([]byte, error) {
	return tc.msgParser.Read(tc)
}

func (tc *TCPConn) WriteMsg(args ...[]byte) error {
	return tc.msgParser.Write(tc, args...)
}
