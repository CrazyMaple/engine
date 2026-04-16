package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	engerr "engine/errors"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

// MongoMailConfig MongoDB 邮件存储配置
type MongoMailConfig struct {
	Session    *mgo.Session  // 已有的 mgo.Session（可复用 persistence 层连接）
	Database   string        // 数据库名
	Collection string        // 集合名（默认 "mails"）
	MaxRetries int           // 最大重试次数（默认 3）
	RetryDelay time.Duration // 重试间隔（默认 500ms）
}

func (c *MongoMailConfig) defaults() {
	if c.Collection == "" {
		c.Collection = "mails"
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = 500 * time.Millisecond
	}
}

// mongoMailDoc MongoDB 邮件文档
type mongoMailDoc struct {
	ID         string    `bson:"_id"`        // mailID
	PlayerID   string    `bson:"player_id"`
	MailJSON   string    `bson:"mail"`       // JSON 序列化的 Mail
	Read       bool      `bson:"read"`
	ExpireTime time.Time `bson:"expire_time"`
	CreateTime time.Time `bson:"create_time"`
}

// MongoMailStorage MongoDB 邮件存储
type MongoMailStorage struct {
	session    *mgo.Session
	db         string
	collection string
	config     MongoMailConfig
}

// NewMongoMailStorage 创建 MongoDB 邮件存储
func NewMongoMailStorage(cfg MongoMailConfig) (*MongoMailStorage, error) {
	cfg.defaults()
	if cfg.Session == nil {
		return nil, fmt.Errorf("mgo session required")
	}
	if cfg.Database == "" {
		return nil, fmt.Errorf("database name required")
	}

	ms := &MongoMailStorage{
		session:    cfg.Session,
		db:         cfg.Database,
		collection: cfg.Collection,
		config:     cfg,
	}

	// 创建索引
	if err := ms.ensureIndexes(); err != nil {
		return nil, fmt.Errorf("ensure indexes: %w", err)
	}

	return ms, nil
}

func (ms *MongoMailStorage) ensureIndexes() error {
	s := ms.session.Copy()
	defer s.Close()
	c := s.DB(ms.db).C(ms.collection)

	return c.EnsureIndex(mgo.Index{
		Key: []string{"player_id", "-create_time"},
	})
}

// withRetry 重试包装器
func (ms *MongoMailStorage) withRetry(op func(c *mgo.Collection) error) error {
	var lastErr error
	for i := 0; i < ms.config.MaxRetries; i++ {
		s := ms.session.Copy()
		err := op(s.DB(ms.db).C(ms.collection))
		s.Close()

		if err == nil {
			return nil
		}
		lastErr = err
		if err == mgo.ErrNotFound {
			return engerr.ErrNotFound
		}
		if i < ms.config.MaxRetries-1 {
			time.Sleep(ms.config.RetryDelay)
		}
	}
	return lastErr
}

func (ms *MongoMailStorage) Save(_ context.Context, playerID string, mail *Mail) error {
	data, err := json.Marshal(mail)
	if err != nil {
		return err
	}

	return ms.withRetry(func(c *mgo.Collection) error {
		_, err := c.UpsertId(mail.ID, bson.M{
			"$set": mongoMailDoc{
				ID:         mail.ID,
				PlayerID:   playerID,
				MailJSON:   string(data),
				Read:       mail.Read,
				ExpireTime: mail.ExpireTime,
				CreateTime: mail.CreateTime,
			},
		})
		return err
	})
}

func (ms *MongoMailStorage) Load(_ context.Context, playerID string) ([]*Mail, error) {
	var result []*Mail
	err := ms.withRetry(func(c *mgo.Collection) error {
		var docs []mongoMailDoc
		if err := c.Find(bson.M{"player_id": playerID}).
			Sort("-create_time").All(&docs); err != nil {
			return err
		}
		result = make([]*Mail, 0, len(docs))
		for _, doc := range docs {
			var m Mail
			if err := json.Unmarshal([]byte(doc.MailJSON), &m); err != nil {
				return fmt.Errorf("unmarshal mail %s: %w", doc.ID, err)
			}
			result = append(result, &m)
		}
		return nil
	})
	return result, err
}

func (ms *MongoMailStorage) MarkRead(_ context.Context, playerID string, mailID string) error {
	return ms.withRetry(func(c *mgo.Collection) error {
		return c.Update(
			bson.M{"_id": mailID, "player_id": playerID},
			bson.M{"$set": bson.M{"read": true}},
		)
	})
}

func (ms *MongoMailStorage) Delete(_ context.Context, playerID string, mailID string) error {
	return ms.withRetry(func(c *mgo.Collection) error {
		return c.Remove(bson.M{"_id": mailID, "player_id": playerID})
	})
}

func (ms *MongoMailStorage) CleanExpired(_ context.Context) (int, error) {
	var removed int
	err := ms.withRetry(func(c *mgo.Collection) error {
		info, err := c.RemoveAll(bson.M{
			"expire_time": bson.M{"$gt": time.Time{}, "$lte": time.Now()},
		})
		if err != nil {
			return err
		}
		removed = info.Removed
		return nil
	})
	return removed, err
}

// Close 关闭存储（不关闭外部传入的 session）
func (ms *MongoMailStorage) Close() {
	// session 由外部管理，此处不关闭
}
