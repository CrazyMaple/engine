// Package k8s 实现 engine/cluster.Provider，通过 in-cluster ServiceAccount
// 调用 Kubernetes Endpoints API 发现集群成员。
//
// 注册 / 注销由 K8s Service selector 自动完成；Register/Deregister 仅满足
// cluster.RegistrableProvider 接口约束，实际不发请求。
package k8s

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"engine/cluster"
	"engine/log"
)

const (
	tokenPath     = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	namespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	apiServer     = "https://kubernetes.default.svc"
)

// Config K8s Provider 配置。
type Config struct {
	// Namespace K8s 命名空间（空则自动从 ServiceAccount 读取）。
	Namespace string
	// ServiceName K8s Service 名称（空则使用 Cluster.Start 传入的 clusterName）。
	ServiceName string
	// PortName 端口名称，用于从 Endpoints 中选择端口。
	PortName string
	// APIServer K8s API Server 地址（空则使用 in-cluster 默认）。测试可注入 httptest URL。
	APIServer string
}

// Provider 基于 K8s Endpoints API 的集群成员发现。
type Provider struct {
	apiServer  string
	portName   string
	httpClient *http.Client

	mu          sync.Mutex
	namespace   string
	serviceName string
	token       string
	onChange    func([]*cluster.Member)
	stopChan    chan struct{}
	started     bool
}

// New 创建 K8s Provider。
func New(cfg Config) *Provider {
	api := cfg.APIServer
	if api == "" {
		api = apiServer
	}
	return &Provider{
		apiServer:   api,
		namespace:   cfg.Namespace,
		serviceName: cfg.ServiceName,
		portName:    cfg.PortName,
	}
}

// Start 实现 cluster.Provider。
func (p *Provider) Start(clusterName string, _ *cluster.Member, onChange func([]*cluster.Member)) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.onChange = onChange
	if p.serviceName == "" {
		p.serviceName = clusterName
	}
	p.stopChan = make(chan struct{})
	p.mu.Unlock()

	if token, err := os.ReadFile(tokenPath); err == nil {
		p.token = strings.TrimSpace(string(token))
	}

	if p.namespace == "" {
		if ns, err := os.ReadFile(namespacePath); err == nil {
			p.namespace = strings.TrimSpace(string(ns))
		}
	}

	p.httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// in-cluster CA 默认不加载；测试侧使用 httptest TLS 也用此选项。
				InsecureSkipVerify: true,
			},
		},
	}

	go p.watchLoop()

	log.Info("[k8s] started discovery in namespace=%s, service=%s", p.namespace, p.serviceName)
	return nil
}

// Stop 实现 cluster.Provider；幂等。
func (p *Provider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		return nil
	}
	p.started = false
	if p.stopChan != nil {
		close(p.stopChan)
		p.stopChan = nil
	}
	return nil
}

// Register K8s 中通过 Service selector 自动注册——空实现以满足扩展接口约束。
func (p *Provider) Register(*cluster.Member) error { return nil }

// Deregister K8s 中 Pod 终止后自动移除——空实现以满足扩展接口约束。
func (p *Provider) Deregister(*cluster.Member) error { return nil }

// GetMembers 实现 cluster.ListableProvider。
func (p *Provider) GetMembers() ([]*cluster.Member, error) {
	p.mu.Lock()
	ns := p.namespace
	svc := p.serviceName
	api := p.apiServer
	p.mu.Unlock()

	url := fmt.Sprintf("%s/api/v1/namespaces/%s/endpoints/%s", api, ns, svc)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query endpoints: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endpoints API error: %s - %s", resp.Status, string(body))
	}

	var endpoints k8sEndpoints
	if err := json.Unmarshal(body, &endpoints); err != nil {
		return nil, fmt.Errorf("unmarshal endpoints: %w", err)
	}

	now := time.Now()
	members := make([]*cluster.Member, 0)
	for _, subset := range endpoints.Subsets {
		port := p.findPort(subset.Ports)
		for _, addr := range subset.Addresses {
			nodeID := addr.TargetRef.Name
			if nodeID == "" {
				nodeID = addr.IP
			}
			members = append(members, &cluster.Member{
				Address:  fmt.Sprintf("%s:%d", addr.IP, port),
				Id:       nodeID,
				Status:   cluster.MemberAlive,
				Seq:      1,
				LastSeen: now,
			})
		}
	}
	return members, nil
}

func (p *Provider) findPort(ports []k8sPort) int {
	if len(ports) == 0 {
		return 0
	}
	if p.portName != "" {
		for _, port := range ports {
			if port.Name == p.portName {
				return port.Port
			}
		}
	}
	return ports[0].Port
}

func (p *Provider) watchLoop() {
	p.mu.Lock()
	stop := p.stopChan
	p.mu.Unlock()
	if stop == nil {
		return
	}
	for {
		select {
		case <-stop:
			return
		default:
		}

		members, err := p.GetMembers()
		if err != nil {
			log.Debug("[k8s] watch error: %v", err)
		} else {
			p.mu.Lock()
			cb := p.onChange
			p.mu.Unlock()
			if cb != nil {
				cb(members)
			}
		}

		select {
		case <-time.After(5 * time.Second):
		case <-stop:
			return
		}
	}
}

// --- K8s API 响应结构 ---

type k8sEndpoints struct {
	Subsets []k8sSubset `json:"subsets"`
}

type k8sSubset struct {
	Addresses []k8sAddress `json:"addresses"`
	Ports     []k8sPort    `json:"ports"`
}

type k8sAddress struct {
	IP        string       `json:"ip"`
	TargetRef k8sTargetRef `json:"targetRef"`
}

type k8sTargetRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type k8sPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// 编译期断言
var (
	_ cluster.Provider            = (*Provider)(nil)
	_ cluster.RegistrableProvider = (*Provider)(nil)
	_ cluster.ListableProvider    = (*Provider)(nil)
)
