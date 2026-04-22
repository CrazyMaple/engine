package network

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
)

// TLSConfig TLS 配置
type TLSConfig struct {
	// CertFile 证书文件路径
	CertFile string
	// KeyFile 私钥文件路径
	KeyFile string
	// CACert CA 证书文件路径（用于双向 TLS 验证客户端）
	CACert string
	// InsecureSkipVerify 是否跳过证书验证（仅用于测试）
	InsecureSkipVerify bool
	// Config 自定义 TLS 配置，如果设置则覆盖上述字段
	Config *tls.Config
}

// newTLSListener 创建 TLS 监听器
func newTLSListener(addr string, cfg *TLSConfig) (net.Listener, error) {
	tlsCfg, err := buildTLSConfig(cfg, true)
	if err != nil {
		return nil, fmt.Errorf("build TLS config: %w", err)
	}
	return tls.Listen("tcp", addr, tlsCfg)
}

// tlsDial 使用 TLS 建立连接
func tlsDial(addr string, cfg *TLSConfig) (net.Conn, error) {
	tlsCfg, err := buildTLSConfig(cfg, false)
	if err != nil {
		return nil, fmt.Errorf("build TLS config: %w", err)
	}
	return tls.Dial("tcp", addr, tlsCfg)
}

// buildTLSConfig 构建 tls.Config
func buildTLSConfig(cfg *TLSConfig, isServer bool) (*tls.Config, error) {
	if cfg.Config != nil {
		return cfg.Config, nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// 加载证书
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load key pair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// 加载 CA 证书（双向 TLS）
	if cfg.CACert != "" {
		caCert, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("invalid CA cert")
		}
		if isServer {
			tlsCfg.ClientCAs = pool
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			tlsCfg.RootCAs = pool
		}
	}

	if cfg.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
	}

	return tlsCfg, nil
}
