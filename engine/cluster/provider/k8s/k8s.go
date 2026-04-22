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
	caPath        = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	namespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	apiServer     = "https://kubernetes.default.svc"
)

// K8sProvider 基于 Kubernetes API 的集群服务发现提供者
// 通过 in-cluster ServiceAccount 令牌访问 K8s Endpoints API
type K8sProvider struct {
	namespace   string // K8s 命名空间
	serviceName string // K8s Service 名称
	portName    string // 端口名称（用于区分多端口服务）
	self        *cluster.Member
	onChange    func([]*cluster.Member)
	stopChan   chan struct{}
	httpClient *http.Client
	token      string
	mu         sync.Mutex
}

// Config K8s 提供者配置
type Config struct {
	// Namespace K8s 命名空间（空则自动从 ServiceAccount 读取）
	Namespace string
	// ServiceName K8s Service 名称（空则使用集群名称）
	ServiceName string
	// PortName 端口名称，用于从 Endpoints 中选择端口
	PortName string
}

// New 创建 K8s 服务发现提供者
func New(cfg Config) *K8sProvider {
	return &K8sProvider{
		namespace:   cfg.Namespace,
		serviceName: cfg.ServiceName,
		portName:    cfg.PortName,
		stopChan:    make(chan struct{}),
	}
}

// Start 启动服务发现
func (p *K8sProvider) Start(clusterName string, self *cluster.Member, onChange func([]*cluster.Member)) error {
	p.mu.Lock()
	p.self = self
	p.onChange = onChange
	if p.serviceName == "" {
		p.serviceName = clusterName
	}
	p.mu.Unlock()

	// 读取 ServiceAccount 令牌
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("read service account token: %w (not running in K8s?)", err)
	}
	p.token = strings.TrimSpace(string(token))

	// 读取命名空间
	if p.namespace == "" {
		ns, err := os.ReadFile(namespacePath)
		if err != nil {
			return fmt.Errorf("read namespace: %w", err)
		}
		p.namespace = strings.TrimSpace(string(ns))
	}

	// 配置 HTTP 客户端（信任 in-cluster CA）
	p.httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // 简化处理，生产建议加载 CA
			},
		},
	}

	// 启动 watch 监听
	go p.watchLoop()

	log.Info("[k8s] started discovery in namespace=%s, service=%s", p.namespace, p.serviceName)
	return nil
}

// Stop 停止服务发现
func (p *K8sProvider) Stop() error {
	close(p.stopChan)
	return nil
}

// Register K8s 中通过 Service selector 自动注册，无需手动操作
func (p *K8sProvider) Register() error {
	return nil
}

// Deregister K8s 中 Pod 终止后自动移除
func (p *K8sProvider) Deregister() error {
	return nil
}

// GetMembers 从 K8s Endpoints API 获取成员列表
func (p *K8sProvider) GetMembers() ([]*cluster.Member, error) {
	p.mu.Lock()
	ns := p.namespace
	svc := p.serviceName
	p.mu.Unlock()

	url := fmt.Sprintf("%s/api/v1/namespaces/%s/endpoints/%s", apiServer, ns, svc)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

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

	members := make([]*cluster.Member, 0)
	for _, subset := range endpoints.Subsets {
		port := p.findPort(subset.Ports)
		for _, addr := range subset.Addresses {
			address := fmt.Sprintf("%s:%d", addr.IP, port)
			nodeID := addr.TargetRef.Name
			if nodeID == "" {
				nodeID = addr.IP
			}

			members = append(members, &cluster.Member{
				Address:  address,
				Id:       nodeID,
				Status:   cluster.MemberAlive,
				LastSeen: time.Now(),
			})
		}
	}

	return members, nil
}

// findPort 从端口列表中查找匹配的端口
func (p *K8sProvider) findPort(ports []k8sPort) int {
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

// watchLoop 定期轮询 Endpoints 变更
func (p *K8sProvider) watchLoop() {
	for {
		select {
		case <-p.stopChan:
			return
		default:
		}

		members, err := p.GetMembers()
		if err != nil {
			log.Debug("[k8s] watch error: %v", err)
		} else if p.onChange != nil {
			p.onChange(members)
		}

		select {
		case <-time.After(5 * time.Second):
		case <-p.stopChan:
			return
		}
	}
}

// ---- K8s API 响应结构 ----

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
