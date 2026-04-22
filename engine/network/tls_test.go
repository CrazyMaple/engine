package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// generateSelfSignedCert 生成自签名证书用于测试
func generateSelfSignedCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatal(err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	keyOut.Close()

	return certFile, keyFile
}

func TestTLSServerClient(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, dir)

	tlsCfg := &TLSConfig{
		CertFile:           certFile,
		KeyFile:            keyFile,
		InsecureSkipVerify: true,
	}

	var received []byte
	var mu sync.Mutex
	done := make(chan struct{})

	server := &TCPServer{
		Addr:            "127.0.0.1:0",
		MaxConnNum:      10,
		PendingWriteNum: 10,
		LenMsgLen:       2,
		MaxMsgLen:       4096,
		TLSCfg:          tlsCfg,
		NewAgent: func(conn *TCPConn) Agent {
			return &testTLSAgent{
				conn: conn,
				onMsg: func(data []byte) {
					mu.Lock()
					received = data
					mu.Unlock()
					close(done)
				},
			}
		},
	}

	// 手动 init 以获取监听地址
	server.init()
	go server.run()
	defer server.Close()

	addr := server.ln.Addr().String()

	// 客户端连接
	clientDone := make(chan struct{})
	client := &TCPClient{
		Addr:            addr,
		ConnNum:         1,
		ConnectInterval: time.Second,
		PendingWriteNum: 10,
		TLSCfg:          tlsCfg,
		LenMsgLen:       2,
		MaxMsgLen:       4096,
		NewAgent: func(conn *TCPConn) Agent {
			return &testTLSSendAgent{
				conn:    conn,
				done:    clientDone,
				message: []byte("hello TLS"),
			}
		},
	}
	client.Start()
	defer client.Close()

	select {
	case <-done:
		mu.Lock()
		if string(received) != "hello TLS" {
			t.Fatalf("expected 'hello TLS', got '%s'", string(received))
		}
		mu.Unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for TLS message")
	}
}

// testTLSAgent 测试用服务端 Agent
type testTLSAgent struct {
	conn  *TCPConn
	onMsg func([]byte)
}

func (a *testTLSAgent) Run() {
	data, err := a.conn.ReadMsg()
	if err != nil {
		return
	}
	if a.onMsg != nil {
		a.onMsg(data)
	}
}

func (a *testTLSAgent) OnClose() {}

// testTLSSendAgent 测试用客户端 Agent
type testTLSSendAgent struct {
	conn    *TCPConn
	done    chan struct{}
	message []byte
}

func (a *testTLSSendAgent) Run() {
	a.conn.WriteMsg(a.message)
	time.Sleep(100 * time.Millisecond)
}

func (a *testTLSSendAgent) OnClose() {}
