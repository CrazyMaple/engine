package mail

import "context"

// MailStorage 邮件存储接口
// 支持可插拔后端：内存（开发/测试）、MongoDB（生产）等
type MailStorage interface {
	// Save 保存一封邮件
	Save(ctx context.Context, playerID string, mail *Mail) error
	// Load 加载玩家所有邮件
	Load(ctx context.Context, playerID string) ([]*Mail, error)
	// MarkRead 标记已读
	MarkRead(ctx context.Context, playerID string, mailID string) error
	// Delete 删除邮件
	Delete(ctx context.Context, playerID string, mailID string) error
	// CleanExpired 清理全部过期邮件，返回清理数量
	CleanExpired(ctx context.Context) (int, error)
}
