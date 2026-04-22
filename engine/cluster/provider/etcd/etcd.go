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

// EtcdProvider 基于 etcd v3 HTTP gateway 的集群服务发现提供者
// 无外部依赖，通过 etcd 的 gRPC-gateway REST API 交互
type EtcdProvider struct {
	endpoints   []string // etcd 节点地址，如 ["http://127.0.0.1:2379"]
	prefix      string   // key 前缀，如 "/maplewish/clusters/{clusterName}/members/"
	self        *cluster.Member
	onChange    func([]*cluster.Member)
	stopChan   chan struct{}
	httpClient *http.Client
	ttl        int64 // 租约 TTL（秒）
	leaseID    int64
	mu         sync.Mutex
}

// New 创建 etcd 服务发现提供者
func New(endpoints ...string) *EtcdProvider {
	if len(endpoints) == 0 {
		endpoints = []string{"http://127.0.0.1:2379"}
	}
	return &EtcdProvider{
		endpoints:  endpoints,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		stopChan:   make(chan struct{}),
		ttl:        15,
	}
}

// Start 启动服务发现
func (p *EtcdProvider) Start(clusterName string, self *cluster.Member, onChange func([]*cluster.Member)) error {
	p.mu.Lock()
	p.prefix = fmt.Sprintf("/maplewish/clusters/%s/members/", clusterName)
	p.self = self
	p.onChange = onChange
	p.mu.Unlock()

	// 启动 watch 监听变更
	go p.watchLoop()

	return nil
}

// Stop 停止服务发现
func (p *EtcdProvider) Stop() error {
	close(p.stopChan)
	return nil
}

// Register 注册本节点到 etcd
func (p *EtcdProvider) Register() error {
	// 1. 创建租约
	leaseID, err := p.grantLease(p.ttl)
	if err != nil {
		return fmt.Errorf("grant lease: %w", err)
	}
	p.mu.Lock()
	p.leaseID = leaseID
	p.mu.Unlock()

	// 2. 写入节点信息
	if err := p.putMember(); err != nil {
		return fmt.Errorf("put member: %w", err)
	}

	// 3. 启动租约续约
	go p.keepAliveLoop()

	log.Info("[etcd] registered member: %s", p.self.Address)
	return nil
}

// Deregister 从 etcd 移除本节点
func (p *EtcdProvider) Deregister() error {
	p.mu.Lock()
	key := p.prefix + p.self.Id
	endpoint := p.endpoints[0]
	p.mu.Unlock()

	return p.deleteKey(endpoint, key)
}

// GetMembers 获取当前成员列表
func (p *EtcdProvider) GetMembers() ([]*cluster.Member, error) {
	p.mu.Lock()
	prefix := p.prefix
	endpoint := p.endpoints[0]
	p.mu.Unlock()

	return p.rangeMembers(endpoint, prefix)
}

// ---- etcd v3 HTTP gateway 交互 ----

// grantLease 创建租约
func (p *EtcdProvider) grantLease(ttl int64) (int64, error) {
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

// keepAliveLoop 定期续约
func (p *EtcdProvider) keepAliveLoop() {
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
		case <-p.stopChan:
			return
		}
	}
}

// putMember 写入成员信息
func (p *EtcdProvider) putMember() error {
	p.mu.Lock()
	self := p.self
	key := p.prefix + self.Id
	leaseID := p.leaseID
	endpoint := p.endpoints[0]
	p.mu.Unlock()

	memberData := memberInfo{
		Address: self.Address,
		Id:      self.Id,
		Kinds:   self.Kinds,
	}
	value, _ := json.Marshal(memberData)

	// etcd v3 API 要求 key/value 是 base64 编码
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

// deleteKey 删除 key
func (p *EtcdProvider) deleteKey(endpoint, key string) error {
	reqBody := map[string]interface{}{
		"key": encodeBase64(key),
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v3/kv/deleterange", endpoint)
	resp, err := p.httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// rangeMembers 查询前缀下的所有成员
func (p *EtcdProvider) rangeMembers(endpoint, prefix string) ([]*cluster.Member, error) {
	// range_end = prefix 的下一个字节序
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
	for _, kv := range result.Kvs {
		decoded := decodeBase64(kv.Value)
		var info memberInfo
		if err := json.Unmarshal([]byte(decoded), &info); err != nil {
			continue
		}
		members = append(members, &cluster.Member{
			Address:  info.Address,
			Id:       info.Id,
			Kinds:    info.Kinds,
			Status:   cluster.MemberAlive,
			LastSeen: time.Now(),
		})
	}
	return members, nil
}

// watchLoop 监听 key 变更
func (p *EtcdProvider) watchLoop() {
	for {
		select {
		case <-p.stopChan:
			return
		default:
		}

		members, err := p.GetMembers()
		if err != nil {
			log.Debug("[etcd] watch error: %v", err)
		} else if p.onChange != nil {
			p.onChange(members)
		}

		// 轮询间隔
		select {
		case <-time.After(3 * time.Second):
		case <-p.stopChan:
			return
		}
	}
}

// ---- 辅助类型和函数 ----

type memberInfo struct {
	Address string   `json:"address"`
	Id      string   `json:"id"`
	Kinds   []string `json:"kinds"`
}

func encodeBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

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
