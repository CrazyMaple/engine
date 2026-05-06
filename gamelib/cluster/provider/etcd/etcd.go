// Package etcd 实现 engine/cluster.Provider，把 etcd v3 当作集群成员发现后端。
//
// 通过 etcd 的 gRPC-gateway REST API 交互，无外部依赖。
package etcd

import (
	"encoding/base64"
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

// Provider 基于 etcd v3 HTTP gateway 的集群成员发现。
type Provider struct {
	endpoints  []string
	httpClient *http.Client
	ttl        int64

	mu       sync.Mutex
	prefix   string
	self     *cluster.Member
	onChange func([]*cluster.Member)
	stopChan chan struct{}
	leaseID  int64
	started  bool
}

// New 创建 etcd Provider。endpoints 为空时默认 "http://127.0.0.1:2379"。
func New(endpoints ...string) *Provider {
	if len(endpoints) == 0 {
		endpoints = []string{"http://127.0.0.1:2379"}
	}
	return &Provider{
		endpoints:  endpoints,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ttl:        15,
	}
}

// Start 实现 cluster.Provider。
func (p *Provider) Start(clusterName string, self *cluster.Member, onChange func([]*cluster.Member)) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.prefix = fmt.Sprintf("/maplewish/clusters/%s/members/", clusterName)
	p.self = self
	p.onChange = onChange
	p.stopChan = make(chan struct{})
	p.mu.Unlock()

	go p.watchLoop()
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
	leaseID, err := p.grantLease(p.ttl)
	if err != nil {
		return fmt.Errorf("grant lease: %w", err)
	}
	p.mu.Lock()
	p.leaseID = leaseID
	p.self = self
	p.mu.Unlock()

	if err := p.putMember(); err != nil {
		return fmt.Errorf("put member: %w", err)
	}

	go p.keepAliveLoop()
	log.Info("[etcd] registered member: %s", self.Address)
	return nil
}

// Deregister 实现 cluster.RegistrableProvider。
func (p *Provider) Deregister(self *cluster.Member) error {
	p.mu.Lock()
	key := p.prefix + self.Id
	endpoint := p.endpoints[0]
	p.mu.Unlock()
	return p.deleteKey(endpoint, key)
}

// GetMembers 实现 cluster.ListableProvider。
func (p *Provider) GetMembers() ([]*cluster.Member, error) {
	p.mu.Lock()
	prefix := p.prefix
	endpoint := p.endpoints[0]
	p.mu.Unlock()
	return p.rangeMembers(endpoint, prefix)
}

// --- etcd v3 HTTP gateway 交互 ---

func (p *Provider) grantLease(ttl int64) (int64, error) {
	body := fmt.Sprintf(`{"TTL": %d}`, ttl)
	url := fmt.Sprintf("%s/v3/lease/grant", p.endpoints[0])

	resp, err := p.httpClient.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		ID int64 `json:"ID,string"`
	}
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, fmt.Errorf("unmarshal lease response: %w, body: %s", err, string(data))
	}
	return result.ID, nil
}

func (p *Provider) keepAliveLoop() {
	p.mu.Lock()
	stop := p.stopChan
	p.mu.Unlock()
	if stop == nil {
		return
	}

	ticker := time.NewTicker(time.Duration(p.ttl/3) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			leaseID := p.leaseID
			endpoint := p.endpoints[0]
			p.mu.Unlock()

			body := fmt.Sprintf(`{"ID": %d}`, leaseID)
			url := fmt.Sprintf("%s/v3/lease/keepalive", endpoint)
			resp, err := p.httpClient.Post(url, "application/json", strings.NewReader(body))
			if err != nil {
				log.Debug("[etcd] keepalive error: %v", err)
				continue
			}
			resp.Body.Close()
		case <-stop:
			return
		}
	}
}

func (p *Provider) putMember() error {
	p.mu.Lock()
	self := p.self
	key := p.prefix + self.Id
	leaseID := p.leaseID
	endpoint := p.endpoints[0]
	p.mu.Unlock()

	value, _ := json.Marshal(memberInfo{
		Address: self.Address,
		Id:      self.Id,
		Kinds:   self.Kinds,
	})

	reqBody := map[string]interface{}{
		"key":   encodeBase64(key),
		"value": encodeBase64(string(value)),
		"lease": leaseID,
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v3/kv/put", endpoint)
	resp, err := p.httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put failed: %s - %s", resp.Status, string(data))
	}
	return nil
}

func (p *Provider) deleteKey(endpoint, key string) error {
	reqBody := map[string]interface{}{"key": encodeBase64(key)}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v3/kv/deleterange", endpoint)
	resp, err := p.httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (p *Provider) rangeMembers(endpoint, prefix string) ([]*cluster.Member, error) {
	rangeEnd := prefixEnd(prefix)
	reqBody := map[string]interface{}{
		"key":       encodeBase64(prefix),
		"range_end": encodeBase64(rangeEnd),
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v3/kv/range", endpoint)
	resp, err := p.httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Kvs []struct {
			Value string `json:"value"`
		} `json:"kvs"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	members := make([]*cluster.Member, 0, len(result.Kvs))
	now := time.Now()
	for _, kv := range result.Kvs {
		var info memberInfo
		if err := json.Unmarshal([]byte(decodeBase64(kv.Value)), &info); err != nil {
			continue
		}
		members = append(members, &cluster.Member{
			Address:  info.Address,
			Id:       info.Id,
			Kinds:    info.Kinds,
			Status:   cluster.MemberAlive,
			Seq:      1,
			LastSeen: now,
		})
	}
	return members, nil
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
			log.Debug("[etcd] watch error: %v", err)
		} else {
			p.mu.Lock()
			cb := p.onChange
			p.mu.Unlock()
			if cb != nil {
				cb(members)
			}
		}

		select {
		case <-time.After(3 * time.Second):
		case <-stop:
			return
		}
	}
}

// --- 辅助 ---

type memberInfo struct {
	Address string   `json:"address"`
	Id      string   `json:"id"`
	Kinds   []string `json:"kinds"`
}

func encodeBase64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func decodeBase64(s string) string {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	return string(data)
}

func prefixEnd(prefix string) string {
	end := []byte(prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xff {
			end[i]++
			return string(end[:i+1])
		}
	}
	return string(end)
}

// 编译期断言
var (
	_ cluster.Provider            = (*Provider)(nil)
	_ cluster.RegistrableProvider = (*Provider)(nil)
	_ cluster.ListableProvider    = (*Provider)(nil)
)
