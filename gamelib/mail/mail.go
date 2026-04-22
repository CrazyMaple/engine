package mail

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// --- 核心数据结构 ---

// Attachment 邮件附件
type Attachment struct {
	Type     string `json:"type"`     // 道具类型
	ItemID   string `json:"item_id"`  // 道具 ID
	Count    int64  `json:"count"`    // 数量
	Metadata string `json:"metadata,omitempty"`
}

// Mail 邮件消息
type Mail struct {
	ID          string       `json:"id"`
	SenderID    string       `json:"sender_id"` // 发送者（玩家ID 或 "system"）
	SenderName  string       `json:"sender_name"`
	ReceiverID  string       `json:"receiver_id"`
	Subject     string       `json:"subject"`
	Content     string       `json:"content"`
	Attachments []Attachment `json:"attachments,omitempty"`
	CreateTime  time.Time    `json:"create_time"`
	ExpireTime  time.Time    `json:"expire_time"`
	Read        bool         `json:"read"`
	Claimed     bool         `json:"claimed"` // 附件是否已领取
}

// IsExpired 检查邮件是否已过期
func (m *Mail) IsExpired(now time.Time) bool {
	return !m.ExpireTime.IsZero() && now.After(m.ExpireTime)
}

// HasAttachments 检查是否有未领取的附件
func (m *Mail) HasAttachments() bool {
	return len(m.Attachments) > 0 && !m.Claimed
}

// --- 消息模板 ---

// Template 消息模板，支持参数化
type Template struct {
	ID      string // 模板ID
	Subject string // 主题模板，支持 {参数名} 占位符
	Content string // 内容模板，支持 {参数名} 占位符
}

// Render 用参数渲染模板
func (t *Template) Render(params map[string]string) (subject, content string) {
	subject = applyTemplate(t.Subject, params)
	content = applyTemplate(t.Content, params)
	return
}

func applyTemplate(tmpl string, params map[string]string) string {
	result := tmpl
	for k, v := range params {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

// TemplateRegistry 模板注册表
type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*Template
}

// NewTemplateRegistry 创建模板注册表
func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{
		templates: make(map[string]*Template),
	}
}

// Register 注册模板
func (tr *TemplateRegistry) Register(t *Template) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.templates[t.ID] = t
}

// Get 获取模板
func (tr *TemplateRegistry) Get(id string) (*Template, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	t, ok := tr.templates[id]
	return t, ok
}

// --- 玩家邮箱 ---

// Mailbox 单个玩家的游戏邮箱
type Mailbox struct {
	PlayerID string
	mails    []*Mail
	mu       sync.RWMutex
	maxSize  int // 最大存储数量（0=不限制）
}

// NewMailbox 创建邮箱
func NewMailbox(playerID string, maxSize int) *Mailbox {
	return &Mailbox{
		PlayerID: playerID,
		mails:    make([]*Mail, 0),
		maxSize:  maxSize,
	}
}

// Add 添加邮件
func (mb *Mailbox) Add(mail *Mail) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.maxSize > 0 && len(mb.mails) >= mb.maxSize {
		// 移除最旧的已读无附件邮件
		for i := 0; i < len(mb.mails); i++ {
			if mb.mails[i].Read && !mb.mails[i].HasAttachments() {
				mb.mails = append(mb.mails[:i], mb.mails[i+1:]...)
				break
			}
		}
		if len(mb.mails) >= mb.maxSize {
			return fmt.Errorf("mailbox full")
		}
	}

	mb.mails = append(mb.mails, mail)
	return nil
}

// List 列出全部邮件（副本）
func (mb *Mailbox) List() []*Mail {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	result := make([]*Mail, len(mb.mails))
	copy(result, mb.mails)
	return result
}

// Get 获取单封邮件
func (mb *Mailbox) Get(mailID string) (*Mail, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	for _, m := range mb.mails {
		if m.ID == mailID {
			return m, true
		}
	}
	return nil, false
}

// MarkRead 标记已读
func (mb *Mailbox) MarkRead(mailID string) bool {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	for _, m := range mb.mails {
		if m.ID == mailID {
			m.Read = true
			return true
		}
	}
	return false
}

// Claim 领取附件
func (mb *Mailbox) Claim(mailID string) ([]Attachment, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	for _, m := range mb.mails {
		if m.ID != mailID {
			continue
		}
		if m.Claimed {
			return nil, fmt.Errorf("already claimed")
		}
		if len(m.Attachments) == 0 {
			return nil, fmt.Errorf("no attachments")
		}
		m.Claimed = true
		m.Read = true
		return m.Attachments, nil
	}
	return nil, fmt.Errorf("mail not found")
}

// Delete 删除邮件
func (mb *Mailbox) Delete(mailID string) bool {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	for i, m := range mb.mails {
		if m.ID == mailID {
			mb.mails = append(mb.mails[:i], mb.mails[i+1:]...)
			return true
		}
	}
	return false
}

// CleanExpired 清理过期邮件（返回清理数量）
func (mb *Mailbox) CleanExpired(now time.Time) int {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	kept := make([]*Mail, 0, len(mb.mails))
	removed := 0
	for _, m := range mb.mails {
		if m.IsExpired(now) && !m.HasAttachments() {
			removed++
			continue
		}
		kept = append(kept, m)
	}
	mb.mails = kept
	return removed
}

// UnreadCount 未读邮件数
func (mb *Mailbox) UnreadCount() int {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	count := 0
	for _, m := range mb.mails {
		if !m.Read {
			count++
		}
	}
	return count
}

// Size 当前邮件数量
func (mb *Mailbox) Size() int {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return len(mb.mails)
}

// --- 邮件服务 ---

// Service 邮件服务，管理多玩家邮箱和广播
type Service struct {
	mu         sync.RWMutex
	mailboxes  map[string]*Mailbox
	templates  *TemplateRegistry
	nextMailID int64
	maxPerBox  int

	// 离线玩家判定：返回 true 表示玩家当前不在线
	isOfflineFn func(playerID string) bool
}

// ServiceConfig 邮件服务配置
type ServiceConfig struct {
	// MaxPerMailbox 每个邮箱最大存储数（0=不限制）
	MaxPerMailbox int
	// IsOfflineFn 判定玩家是否离线
	IsOfflineFn func(playerID string) bool
}

// NewService 创建邮件服务
func NewService(cfg ServiceConfig) *Service {
	if cfg.IsOfflineFn == nil {
		cfg.IsOfflineFn = func(string) bool { return false }
	}
	return &Service{
		mailboxes:   make(map[string]*Mailbox),
		templates:   NewTemplateRegistry(),
		maxPerBox:   cfg.MaxPerMailbox,
		isOfflineFn: cfg.IsOfflineFn,
	}
}

// Templates 返回模板注册表
func (s *Service) Templates() *TemplateRegistry {
	return s.templates
}

// getOrCreateMailbox 获取或创建玩家邮箱
func (s *Service) getOrCreateMailbox(playerID string) *Mailbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	mb, ok := s.mailboxes[playerID]
	if !ok {
		mb = NewMailbox(playerID, s.maxPerBox)
		s.mailboxes[playerID] = mb
	}
	return mb
}

// generateMailID 生成唯一邮件 ID
func (s *Service) generateMailID() string {
	s.mu.Lock()
	s.nextMailID++
	id := s.nextMailID
	s.mu.Unlock()
	return fmt.Sprintf("mail_%d_%d", time.Now().UnixNano(), id)
}

// Send 发送邮件给指定玩家
func (s *Service) Send(mail *Mail) error {
	if mail.ReceiverID == "" {
		return fmt.Errorf("receiver id required")
	}
	if mail.ID == "" {
		mail.ID = s.generateMailID()
	}
	if mail.CreateTime.IsZero() {
		mail.CreateTime = time.Now()
	}
	mb := s.getOrCreateMailbox(mail.ReceiverID)
	return mb.Add(mail)
}

// SendTemplate 使用模板发送邮件
func (s *Service) SendTemplate(templateID, receiverID string, params map[string]string, attachments []Attachment) error {
	tmpl, ok := s.templates.Get(templateID)
	if !ok {
		return fmt.Errorf("template %s not found", templateID)
	}
	subject, content := tmpl.Render(params)
	return s.Send(&Mail{
		SenderID:    "system",
		SenderName:  "System",
		ReceiverID:  receiverID,
		Subject:     subject,
		Content:     content,
		Attachments: attachments,
		ExpireTime:  time.Now().Add(7 * 24 * time.Hour),
	})
}

// Broadcast 全服广播邮件（需提供玩家 ID 列表）
func (s *Service) Broadcast(mail *Mail, receiverIDs []string) int {
	sent := 0
	for _, pid := range receiverIDs {
		copied := *mail
		copied.ID = ""
		copied.ReceiverID = pid
		if err := s.Send(&copied); err == nil {
			sent++
		}
	}
	return sent
}

// ListMails 列出玩家邮件
func (s *Service) ListMails(playerID string) []*Mail {
	s.mu.RLock()
	mb, ok := s.mailboxes[playerID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	return mb.List()
}

// MarkRead 标记已读
func (s *Service) MarkRead(playerID, mailID string) bool {
	s.mu.RLock()
	mb, ok := s.mailboxes[playerID]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return mb.MarkRead(mailID)
}

// Claim 领取附件
func (s *Service) Claim(playerID, mailID string) ([]Attachment, error) {
	s.mu.RLock()
	mb, ok := s.mailboxes[playerID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mailbox not found")
	}
	return mb.Claim(mailID)
}

// Delete 删除邮件
func (s *Service) Delete(playerID, mailID string) bool {
	s.mu.RLock()
	mb, ok := s.mailboxes[playerID]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return mb.Delete(mailID)
}

// CleanExpiredAll 清理全部过期邮件
func (s *Service) CleanExpiredAll() int {
	s.mu.RLock()
	mbs := make([]*Mailbox, 0, len(s.mailboxes))
	for _, mb := range s.mailboxes {
		mbs = append(mbs, mb)
	}
	s.mu.RUnlock()

	total := 0
	now := time.Now()
	for _, mb := range mbs {
		total += mb.CleanExpired(now)
	}
	return total
}

// --- 通知服务 ---

// NotificationLevel 通知级别
type NotificationLevel int

const (
	NotifyInfo    NotificationLevel = iota // 普通通知
	NotifyWarning                           // 警告
	NotifySystem                            // 系统通知（如维护公告）
)

// Notification 通知消息
type Notification struct {
	ID         string            `json:"id"`
	Level      NotificationLevel `json:"level"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	TargetIDs  []string          `json:"target_ids,omitempty"` // 目标玩家（空=全服广播）
	CreateTime time.Time         `json:"create_time"`
}

// NotificationDeliverFn 通知投递函数（业务层实现，如 WebSocket 推送）
type NotificationDeliverFn func(playerID string, notif *Notification) error

// NotificationService 通知服务
type NotificationService struct {
	deliverFn    NotificationDeliverFn
	offlineQueue map[string][]*Notification // playerID -> 离线通知
	mu           sync.Mutex
	nextID       int64
}

// NewNotificationService 创建通知服务
func NewNotificationService(deliverFn NotificationDeliverFn) *NotificationService {
	return &NotificationService{
		deliverFn:    deliverFn,
		offlineQueue: make(map[string][]*Notification),
	}
}

// Push 推送通知给指定玩家（在线直推，离线入队）
func (ns *NotificationService) Push(notif *Notification) error {
	ns.mu.Lock()
	ns.nextID++
	if notif.ID == "" {
		notif.ID = fmt.Sprintf("notif_%d", ns.nextID)
	}
	if notif.CreateTime.IsZero() {
		notif.CreateTime = time.Now()
	}
	ns.mu.Unlock()

	for _, pid := range notif.TargetIDs {
		if ns.deliverFn != nil {
			if err := ns.deliverFn(pid, notif); err != nil {
				// 投递失败，存入离线队列
				ns.enqueueOffline(pid, notif)
			}
		} else {
			ns.enqueueOffline(pid, notif)
		}
	}
	return nil
}

// enqueueOffline 入队离线通知
func (ns *NotificationService) enqueueOffline(playerID string, notif *Notification) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.offlineQueue[playerID] = append(ns.offlineQueue[playerID], notif)
}

// OnPlayerOnline 玩家上线时拉取离线通知并推送
func (ns *NotificationService) OnPlayerOnline(playerID string) []*Notification {
	ns.mu.Lock()
	queued := ns.offlineQueue[playerID]
	delete(ns.offlineQueue, playerID)
	ns.mu.Unlock()

	if ns.deliverFn != nil {
		for _, n := range queued {
			_ = ns.deliverFn(playerID, n)
		}
	}
	return queued
}

// BroadcastAll 全服广播（需提供在线玩家列表）
func (ns *NotificationService) BroadcastAll(notif *Notification, onlinePlayerIDs []string) {
	for _, pid := range onlinePlayerIDs {
		copied := *notif
		copied.TargetIDs = []string{pid}
		_ = ns.Push(&copied)
	}
}

// OfflineCount 返回指定玩家的离线通知数量
func (ns *NotificationService) OfflineCount(playerID string) int {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return len(ns.offlineQueue[playerID])
}
