package consul

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"engine/cluster"
	"engine/log"
)

// ConsulProvider 基于 Consul HTTP API 的集群服务发现提供者
// 无外部依赖，直接使用 net/http 调用 Consul API
type ConsulProvider struct {
	consulAddr  string // Consul 地址，如 "http://127.0.0.1:8500"
	serviceName string // 服务名称
	self        *cluster.Member
	onChange    func([]*cluster.Member)
	stopChan   chan struct{}
	httpClient *http.Client
	mu         sync.Mutex
}

// New 创建 Consul 服务发现提供者
func New(consulAddr string) *ConsulProvider {
	if !strings.HasPrefix(consulAddr, "http") {
		consulAddr = "http://" + consulAddr
	}
	return &ConsulProvider{
		consulAddr: consulAddr,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		stopChan:   make(chan struct{}),
	}
}

// Start 启动服务发现
func (p *ConsulProvider) Start(clusterName string, self *cluster.Member, onChange func([]*cluster.Member)) error {
	p.mu.Lock()
	p.serviceName = clusterName
	p.self = self
	p.onChange = onChange
	p.mu.Unlock()

	// 启动长轮询监听服务变更
	go p.watchServices()

	return nil
}

// Stop 停止服务发现
func (p *ConsulProvider) Stop() error {
	close(p.stopChan)
	return nil
}

// Register 注册本节点到 Consul
func (p *ConsulProvider) Register() error {
	p.mu.Lock()
	self := p.self
	serviceName := p.serviceName
	p.mu.Unlock()

	host, port := parseAddress(self.Address)

	registration := map[string]interface{}{
		"ID":      self.Id,
		"Name":    serviceName,
		"Address": host,
		"Port":    port,
		"Tags":    self.Kinds,
		"Meta": map[string]string{
			"node_id": self.Id,
			"kinds":   strings.Join(self.Kinds, ","),
		},
		"Check": map[string]interface{}{
			"TCP":                            self.Address,
			"Interval":                       "5s",
			"Timeout":                        "3s",
			"DeregisterCriticalServiceAfter": "30s",
		},
	}

	data, err := json.Marshal(registration)
	if err != nil {
		return fmt.Errorf("marshal registration: %w", err)
	}

	url := fmt.Sprintf("%s/v1/agent/service/register", p.consulAddr)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register failed: %s - %s", resp.Status, string(body))
	}

	log.Info("[consul] registered service: %s (%s)", serviceName, self.Address)
	return nil
}

// Deregister 从 Consul 移除本节点
func (p *ConsulProvider) Deregister() error {
	p.mu.Lock()
	self := p.self
	p.mu.Unlock()

	url := fmt.Sprintf("%s/v1/agent/service/deregister/%s", p.consulAddr, self.Id)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deregister service: %w", err)
	}
	defer resp.Body.Close()

	log.Info("[consul] deregistered service: %s", self.Id)
	return nil
}

// GetMembers 从 Consul 获取当前成员列表
func (p *ConsulProvider) GetMembers() ([]*cluster.Member, error) {
	p.mu.Lock()
	serviceName := p.serviceName
	p.mu.Unlock()

	url := fmt.Sprintf("%s/v1/health/service/%s?passing=true", p.consulAddr, serviceName)
	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var entries []consulServiceEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	members := make([]*cluster.Member, 0, len(entries))
	for _, entry := range entries {
		address := fmt.Sprintf("%s:%d", entry.Service.Address, entry.Service.Port)
		nodeID := entry.Service.Meta["node_id"]
		if nodeID == "" {
			nodeID = entry.Service.ID
		}
		kinds := parseKinds(entry.Service.Meta["kinds"])
		if len(kinds) == 0 {
			kinds = entry.Service.Tags
		}

		members = append(members, &cluster.Member{
			Address:  address,
			Id:       nodeID,
			Kinds:    kinds,
			Status:   cluster.MemberAlive,
			LastSeen: time.Now(),
		})
	}

	return members, nil
}

// watchServices 长轮询监听服务变更
func (p *ConsulProvider) watchServices() {
	var lastIndex string

	for {
		select {
		case <-p.stopChan:
			return
		default:
		}

		p.mu.Lock()
		serviceName := p.serviceName
		p.mu.Unlock()

		url := fmt.Sprintf("%s/v1/health/service/%s?passing=true&wait=30s", p.consulAddr, serviceName)
		if lastIndex != "" {
			url += "&index=" + lastIndex
		}

		resp, err := p.httpClient.Get(url)
		if err != nil {
			log.Debug("[consul] watch error: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		// 更新 Consul index（用于长轮询）
		newIndex := resp.Header.Get("X-Consul-Index")
		if newIndex != "" && newIndex != lastIndex {
			lastIndex = newIndex

			body, _ := io.ReadAll(resp.Body)
			var entries []consulServiceEntry
			if err := json.Unmarshal(body, &entries); err == nil {
				members := make([]*cluster.Member, 0, len(entries))
				for _, entry := range entries {
					address := fmt.Sprintf("%s:%d", entry.Service.Address, entry.Service.Port)
					nodeID := entry.Service.Meta["node_id"]
					if nodeID == "" {
						nodeID = entry.Service.ID
					}
					kinds := parseKinds(entry.Service.Meta["kinds"])

					members = append(members, &cluster.Member{
						Address:  address,
						Id:       nodeID,
						Kinds:    kinds,
						Status:   cluster.MemberAlive,
						LastSeen: time.Now(),
					})
				}

				p.mu.Lock()
				onChange := p.onChange
				p.mu.Unlock()
				if onChange != nil {
					onChange(members)
				}
			}
		}
		resp.Body.Close()
	}
}

// Consul API 响应结构

type consulServiceEntry struct {
	Service consulService `json:"Service"`
}

type consulService struct {
	ID      string            `json:"ID"`
	Service string            `json:"Service"`
	Address string            `json:"Address"`
	Port    int               `json:"Port"`
	Tags    []string          `json:"Tags"`
	Meta    map[string]string `json:"Meta"`
}

// 辅助函数

func parseAddress(addr string) (string, int) {
	parts := strings.SplitN(addr, ":", 2)
	if len(parts) != 2 {
		return addr, 0
	}
	port := 0
	fmt.Sscanf(parts[1], "%d", &port)
	return parts[0], port
}

func parseKinds(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
