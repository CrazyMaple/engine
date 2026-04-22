package mail

import (
	"engine/actor"
)

// --- Actor 消息定义 ---

// SendMailRequest 发送邮件请求
type SendMailRequest struct {
	Mail *Mail
}

// SendMailResponse 发送邮件响应
type SendMailResponse struct {
	MailID string
	Err    string
}

// SendTemplateRequest 使用模板发送邮件请求
type SendTemplateRequest struct {
	TemplateID  string
	ReceiverID  string
	Params      map[string]string
	Attachments []Attachment
}

// ListMailsRequest 列出邮件请求
type ListMailsRequest struct {
	PlayerID string
}

// ListMailsResponse 列出邮件响应
type ListMailsResponse struct {
	Mails []*Mail
}

// ClaimMailRequest 领取附件请求
type ClaimMailRequest struct {
	PlayerID string
	MailID   string
}

// ClaimMailResponse 领取附件响应
type ClaimMailResponse struct {
	Attachments []Attachment
	Err         string
}

// MarkReadRequest 标记已读
type MarkReadRequest struct {
	PlayerID string
	MailID   string
}

// DeleteMailRequest 删除邮件
type DeleteMailRequest struct {
	PlayerID string
	MailID   string
}

// BroadcastRequest 广播邮件
type BroadcastRequest struct {
	Mail       *Mail
	Recipients []string
}

// BroadcastResponse 广播响应
type BroadcastResponse struct {
	Sent int
}

// --- Actor 实现 ---

// MailActor 邮件服务 Actor
type MailActor struct {
	service *Service
}

// NewMailActor 创建邮件 Actor
func NewMailActor(cfg ServiceConfig) *MailActor {
	return &MailActor{
		service: NewService(cfg),
	}
}

// NewMailProps 创建邮件 Actor Props
func NewMailProps(cfg ServiceConfig) *actor.Props {
	return actor.PropsFromProducer(func() actor.Actor {
		return NewMailActor(cfg)
	})
}

// Service 返回内部服务
func (ma *MailActor) Service() *Service {
	return ma.service
}

// Receive 实现 actor.Actor 接口
func (ma *MailActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		return

	case *SendMailRequest:
		err := ma.service.Send(msg.Mail)
		resp := &SendMailResponse{MailID: msg.Mail.ID}
		if err != nil {
			resp.Err = err.Error()
		}
		ctx.Respond(resp)

	case *SendTemplateRequest:
		err := ma.service.SendTemplate(msg.TemplateID, msg.ReceiverID, msg.Params, msg.Attachments)
		resp := &SendMailResponse{}
		if err != nil {
			resp.Err = err.Error()
		}
		ctx.Respond(resp)

	case *ListMailsRequest:
		mails := ma.service.ListMails(msg.PlayerID)
		ctx.Respond(&ListMailsResponse{Mails: mails})

	case *ClaimMailRequest:
		items, err := ma.service.Claim(msg.PlayerID, msg.MailID)
		resp := &ClaimMailResponse{Attachments: items}
		if err != nil {
			resp.Err = err.Error()
		}
		ctx.Respond(resp)

	case *MarkReadRequest:
		ma.service.MarkRead(msg.PlayerID, msg.MailID)

	case *DeleteMailRequest:
		ma.service.Delete(msg.PlayerID, msg.MailID)

	case *BroadcastRequest:
		sent := ma.service.Broadcast(msg.Mail, msg.Recipients)
		ctx.Respond(&BroadcastResponse{Sent: sent})
	}
}
