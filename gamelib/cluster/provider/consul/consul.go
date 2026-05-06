// Package consul 实现 engine/cluster.Provider，把 Consul 当作集群成员发现后端。
//
// 满足 ADR v1.13-001 §2.1 / §2.2 的接口契约：
//   - 主接口：Start / Stop（来自 cluster.Provider）；
//   - 扩展接口：Register / Deregister（来自 cluster.RegistrableProvider）；
//   - 扩展接口：GetMembers（来自 cluster.ListableProvider）。
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

// Provider 基于 Consul HTTP API 的集群成员发现。
//
// 无外部依赖，直接通过 net/http 调用 Consul Agent API。
type Provider struct {
	consulAddr string
	httpClient *http.Client

	mu          sync.Mutex
	serviceName string
	self        *cluster.Member
	onChange    func([]*cluster.Member)
	stopChan    chan struct{}
	started     bool
}

// New 创建 Consul Provider。consulAddr 形如 "127.0.0.1:8500" 或 "http://127.0.0.1:8500"。
func New(consulAddr string) *Provider {
	if !strings.HasPrefix(consulAddr, "http") {
		consulAddr = "http://" + consulAddr
	}
	return &Provider{
		consulAddr: consulAddr,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Start 实现 cluster.Provider。启动后开始长轮询 Consul 服务列表，
// 每次发现成员变更通过 onChange 推送一份"当前活跃成员"全量快照。
func (p *Provider) Start(clusterName string, self *cluster.Member, onChange func([]*cluster.Member)) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.serviceName = clusterName
	p.self = self
	p.onChange = onChange
	p.stopChan = make(chan struct{})
	p.mu.Unlock()

	go p.watchServices()
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

// Register 实现 cluster.RegistrableProvider。
func (p *Provider) Register(self *cluster.Member) error {
	p.mu.Lock()
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

// Deregister 实现 cluster.RegistrableProvider。
func (p *Provider) Deregister(self *cluster.Member) error {
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

// GetMembers 实现 cluster.ListableProvider。
func (p *Provider) GetMembers() ([]*cluster.Member, error) {
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

	return parseEntries(entries), nil
}

// watchServices 长轮询 Consul，监听服务变更。
func (p *Provider) watchServices() {
	var lastIndex string

	for {
		p.mu.Lock()
		stop := p.stopChan
		serviceName := p.serviceName
		onChange := p.onChange
		p.mu.Unlock()

		if stop == nil {
			return
		}
		select {
		case <-stop:
			return
		default:
		}

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

		newIndex := resp.Header.Get("X-Consul-Index")
		if newIndex != "" && newIndex != lastIndex {
			lastIndex = newIndex
			body, _ := io.ReadAll(resp.Body)
			var entries []consulServiceEntry
			if err := json.Unmarshal(body, &entries); err == nil && onChange != nil {
				onChange(parseEntries(entries))
			}
		}
		resp.Body.Close()
	}
}

// --- Consul API 响应结构 ---

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

func parseEntries(entries []consulServiceEntry) []*cluster.Member {
	members := make([]*cluster.Member, 0, len(entries))
	now := time.Now()
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
			Seq:      1,
			LastSeen: now,
		})
	}
	return members
}

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

// 编译期断言：实现三个相关接口。
var (
	_ cluster.Provider            = (*Provider)(nil)
	_ cluster.RegistrableProvider = (*Provider)(nil)
	_ cluster.ListableProvider    = (*Provider)(nil)
)
