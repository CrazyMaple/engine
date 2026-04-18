package network

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// KCPServer 基于可靠 UDP 的 KCP 服务器
//
// 多个客户端会话共享一个 UDP socket，按 (remoteAddr, conv) demux 到独立 KCPConn。
type KCPServer struct {
	// Addr 监听地址，例如 ":9100"
	Addr string
	// MaxConnNum 最大会话数
	MaxConnNum int
	// Config KCP 协议参数
	Config KCPConfig
	// NewAgent 会话建立后回调，业务方返回 Agent 处理消息循环
	NewAgent func(*KCPConn) Agent

	udp      net.PacketConn
	sessions map[string]*KCPConn
	mu       sync.Mutex
	nextConv uint32
	closing  chan struct{}
	wg       sync.WaitGroup
}

// Start 启动服务器
func (s *KCPServer) Start() error {
	if s.NewAgent == nil {
		return errors.New("KCPServer: NewAgent must not be nil")
	}
	if s.Config.Interval <= 0 {
		s.Config = DefaultKCPConfig()
	}
	if s.MaxConnNum <= 0 {
		s.MaxConnNum = 1024
	}
	udp, err := net.ListenPacket("udp", s.Addr)
	if err != nil {
		return err
	}
	s.udp = udp
	s.sessions = make(map[string]*KCPConn)
	s.closing = make(chan struct{})

	s.wg.Add(1)
	go s.recvLoop()
	return nil
}

// LocalAddr 返回实际监听地址（方便测试用动态端口）
func (s *KCPServer) LocalAddr() net.Addr {
	if s.udp == nil {
		return nil
	}
	return s.udp.LocalAddr()
}

func (s *KCPServer) recvLoop() {
	defer s.wg.Done()
	buf := make([]byte, s.Config.Mtu*2)
	for {
		select {
		case <-s.closing:
			return
		default:
		}
		_ = s.udp.SetReadDeadline(time.Now().Add(s.Config.Interval * 4))
		n, addr, err := s.udp.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if n < kcpHeaderSize {
			continue
		}
		dup := make([]byte, n)
		copy(dup, buf[:n])
		s.routePacket(addr, dup)
	}
}

func (s *KCPServer) routePacket(addr net.Addr, buf []byte) {
	key := addr.String()
	s.mu.Lock()
	sess, ok := s.sessions[key]
	if !ok {
		if len(s.sessions) >= s.MaxConnNum {
			s.mu.Unlock()
			return
		}
		conv := binary.LittleEndian.Uint32(buf[0:4])
		if conv == 0 {
			conv = atomic.AddUint32(&s.nextConv, 1)
		}
		sess = newKCPConn(s.udp, addr, conv, s.Config, true)
		s.sessions[key] = sess
		sess.onClose = func() {
			s.mu.Lock()
			delete(s.sessions, key)
			s.mu.Unlock()
		}
		s.mu.Unlock()

		agent := s.NewAgent(sess)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			agent.Run()
			sess.Close()
			agent.OnClose()
		}()
	} else {
		s.mu.Unlock()
	}
	sess.inputForServer(buf)
}

// Close 关闭服务器，等待所有会话退出
func (s *KCPServer) Close() {
	if s.closing == nil {
		return
	}
	select {
	case <-s.closing:
		return
	default:
	}
	close(s.closing)
	if s.udp != nil {
		s.udp.Close()
	}
	s.mu.Lock()
	sessions := make([]*KCPConn, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.sessions = nil
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.Close()
	}
	s.wg.Wait()
}

// SessionCount 当前活跃会话数
func (s *KCPServer) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// DialKCP 客户端拨号建立 KCP 连接
//
// localAddr 可传 ":0" 让系统分配端口；conv 用 nano 时间戳生成唯一会话 ID。
func DialKCP(remoteAddr string, config KCPConfig) (*KCPConn, error) {
	raddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	if config.Interval <= 0 {
		config = DefaultKCPConfig()
	}
	conv := uint32(time.Now().UnixNano())
	if conv == 0 {
		conv = 1
	}
	return newKCPConn(udp, raddr, conv, config, false), nil
}
