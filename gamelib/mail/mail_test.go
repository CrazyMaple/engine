package mail

import (
	"testing"
	"time"
)

func TestMailbox_AddAndList(t *testing.T) {
	mb := NewMailbox("p1", 0)

	mail := &Mail{
		ID:         "m1",
		SenderID:   "system",
		ReceiverID: "p1",
		Subject:    "Welcome",
		Content:    "Hello",
		CreateTime: time.Now(),
	}

	if err := mb.Add(mail); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	mails := mb.List()
	if len(mails) != 1 {
		t.Errorf("list len = %d, want 1", len(mails))
	}
}

func TestMailbox_MarkReadAndClaim(t *testing.T) {
	mb := NewMailbox("p1", 0)

	mail := &Mail{
		ID: "m1",
		Attachments: []Attachment{
			{Type: "gold", ItemID: "gold", Count: 100},
		},
	}
	mb.Add(mail)

	if mb.UnreadCount() != 1 {
		t.Errorf("unread = %d, want 1", mb.UnreadCount())
	}

	mb.MarkRead("m1")
	if mb.UnreadCount() != 0 {
		t.Errorf("unread after mark = %d, want 0", mb.UnreadCount())
	}

	items, err := mb.Claim("m1")
	if err != nil {
		t.Fatalf("Claim error: %v", err)
	}
	if len(items) != 1 || items[0].Count != 100 {
		t.Errorf("claimed = %+v", items)
	}

	// 二次领取应失败
	if _, err := mb.Claim("m1"); err == nil {
		t.Error("double claim should fail")
	}
}

func TestMailbox_CleanExpired(t *testing.T) {
	mb := NewMailbox("p1", 0)

	now := time.Now()
	mb.Add(&Mail{ID: "m1", ExpireTime: now.Add(-time.Hour), Read: true})
	mb.Add(&Mail{ID: "m2", ExpireTime: now.Add(time.Hour)})
	// 带附件的过期邮件不应被清理
	mb.Add(&Mail{ID: "m3", ExpireTime: now.Add(-time.Hour), Attachments: []Attachment{{Count: 1}}})

	removed := mb.CleanExpired(now)
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if mb.Size() != 2 {
		t.Errorf("size after clean = %d, want 2", mb.Size())
	}
}

func TestTemplate_Render(t *testing.T) {
	tmpl := &Template{
		ID:      "welcome",
		Subject: "Welcome, {name}!",
		Content: "You got {amount} gold from {event}.",
	}
	subject, content := tmpl.Render(map[string]string{
		"name":   "Alice",
		"amount": "500",
		"event":  "DailyLogin",
	})
	if subject != "Welcome, Alice!" {
		t.Errorf("subject = %q", subject)
	}
	if content != "You got 500 gold from DailyLogin." {
		t.Errorf("content = %q", content)
	}
}

func TestService_SendAndList(t *testing.T) {
	svc := NewService(ServiceConfig{MaxPerMailbox: 100})

	err := svc.Send(&Mail{
		SenderID:   "system",
		ReceiverID: "p1",
		Subject:    "Hello",
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	mails := svc.ListMails("p1")
	if len(mails) != 1 {
		t.Errorf("list len = %d", len(mails))
	}
	if mails[0].ID == "" {
		t.Error("mail ID not generated")
	}
}

func TestService_SendTemplate(t *testing.T) {
	svc := NewService(ServiceConfig{})
	svc.Templates().Register(&Template{
		ID:      "reward",
		Subject: "Reward: {event}",
		Content: "You got {item} x{count}",
	})

	err := svc.SendTemplate("reward", "p1", map[string]string{
		"event": "Login",
		"item":  "Gold",
		"count": "100",
	}, []Attachment{{Type: "gold", Count: 100}})
	if err != nil {
		t.Fatalf("SendTemplate error: %v", err)
	}

	mails := svc.ListMails("p1")
	if len(mails) != 1 {
		t.Fatalf("list len = %d", len(mails))
	}
	if mails[0].Subject != "Reward: Login" {
		t.Errorf("subject = %q", mails[0].Subject)
	}
	if len(mails[0].Attachments) != 1 {
		t.Error("attachments missing")
	}
}

func TestService_Broadcast(t *testing.T) {
	svc := NewService(ServiceConfig{})

	sent := svc.Broadcast(&Mail{
		SenderID: "system",
		Subject:  "Maintenance",
		Content:  "Server will restart",
	}, []string{"p1", "p2", "p3"})

	if sent != 3 {
		t.Errorf("sent = %d, want 3", sent)
	}
	if len(svc.ListMails("p2")) != 1 {
		t.Error("p2 should have 1 mail")
	}
}

func TestNotificationService_OnlineDeliver(t *testing.T) {
	var delivered []*Notification
	ns := NewNotificationService(func(playerID string, n *Notification) error {
		delivered = append(delivered, n)
		return nil
	})

	ns.Push(&Notification{
		Level:     NotifyInfo,
		Title:     "Hi",
		Content:   "hello",
		TargetIDs: []string{"p1"},
	})

	if len(delivered) != 1 {
		t.Errorf("delivered = %d, want 1", len(delivered))
	}
}

func TestNotificationService_OfflineQueue(t *testing.T) {
	ns := NewNotificationService(func(playerID string, n *Notification) error {
		return ErrOffline
	})

	ns.Push(&Notification{
		Title:     "Msg",
		TargetIDs: []string{"p1"},
	})

	if ns.OfflineCount("p1") != 1 {
		t.Errorf("offline count = %d, want 1", ns.OfflineCount("p1"))
	}

	// 玩家上线后，离线通知被消费
	queued := ns.OnPlayerOnline("p1")
	if len(queued) != 1 {
		t.Errorf("queued at online = %d", len(queued))
	}
	if ns.OfflineCount("p1") != 0 {
		t.Error("offline not cleared after online")
	}
}

// ErrOffline 测试辅助错误
var ErrOffline = &offlineErr{}

type offlineErr struct{}

func (e *offlineErr) Error() string { return "offline" }
