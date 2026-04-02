package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	engerr "engine/errors"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

// MongoConfig MongoDB 连接配置
type MongoConfig struct {
	Addrs      []string      // MongoDB 地址列表
	Database   string        // 数据库名
	Collection string        // 集合名
	Username   string        // 用户名
	Password   string        // 密码
	Timeout    time.Duration // 连接超时（默认 10s）
	PoolLimit  int           // 连接池大小（默认 100）
	MaxRetries int           // 最大重试次数（默认 3）
	RetryDelay time.Duration // 重试间隔（默认 500ms）
}

func (c *MongoConfig) defaults() {
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Second
	}
	if c.PoolLimit <= 0 {
		c.PoolLimit = 100
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = 500 * time.Millisecond
	}
}

// MongoStorage MongoDB 存储后端
type MongoStorage struct {
	session    *mgo.Session
	db         string
	collection string
	config     *MongoConfig // nil 表示通过旧构造函数创建
}

type mongoDocument struct {
	ID        string    `bson:"_id"`
	State     string    `bson:"state"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// NewMongoStorage 创建 MongoDB 存储（旧接口，保持兼容）
func NewMongoStorage(session *mgo.Session, db, collection string) *MongoStorage {
	return &MongoStorage{
		session:    session,
		db:         db,
		collection: collection,
	}
}

// NewMongoStorageFromConfig 从配置创建 MongoDB 存储
func NewMongoStorageFromConfig(cfg MongoConfig) (*MongoStorage, error) {
	cfg.defaults()

	info := &mgo.DialInfo{
		Addrs:    cfg.Addrs,
		Database: cfg.Database,
		Username: cfg.Username,
		Password: cfg.Password,
		Timeout:  cfg.Timeout,
		PoolLimit: cfg.PoolLimit,
	}

	session, err := mgo.DialWithInfo(info)
	if err != nil {
		return nil, &engerr.ConnectError{
			Address: fmt.Sprintf("%v", cfg.Addrs),
			Cause:   err,
		}
	}
	session.SetMode(mgo.Monotonic, true)

	return &MongoStorage{
		session:    session,
		db:         cfg.Database,
		collection: cfg.Collection,
		config:     &cfg,
	}, nil
}

// Close 关闭连接
func (ms *MongoStorage) Close() {
	if ms.session != nil {
		ms.session.Close()
	}
}

// withRetry 重试包装器，每次使用 session.Copy() 获取独立连接
func (ms *MongoStorage) withRetry(op func(c *mgo.Collection) error) error {
	maxRetries := 1
	var retryDelay time.Duration
	if ms.config != nil {
		maxRetries = ms.config.MaxRetries
		retryDelay = ms.config.RetryDelay
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		s := ms.session.Copy()
		err := op(s.DB(ms.db).C(ms.collection))
		s.Close()

		if err == nil {
			return nil
		}
		lastErr = err

		// 不重试非网络类错误
		if err == mgo.ErrNotFound {
			return err
		}

		if i < maxRetries-1 && retryDelay > 0 {
			time.Sleep(retryDelay)
		}
	}
	return lastErr
}

func (ms *MongoStorage) Save(_ context.Context, id string, state interface{}) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return ms.withRetry(func(c *mgo.Collection) error {
		_, err := c.UpsertId(id, bson.M{
			"$set": bson.M{
				"state":      string(data),
				"updated_at": time.Now(),
			},
		})
		return err
	})
}

func (ms *MongoStorage) Load(_ context.Context, id string, target interface{}) error {
	return ms.withRetry(func(c *mgo.Collection) error {
		var doc mongoDocument
		if err := c.FindId(id).One(&doc); err != nil {
			if err == mgo.ErrNotFound {
				return engerr.ErrNotFound
			}
			return err
		}
		return json.Unmarshal([]byte(doc.State), target)
	})
}

func (ms *MongoStorage) Delete(_ context.Context, id string) error {
	return ms.withRetry(func(c *mgo.Collection) error {
		return c.RemoveId(id)
	})
}

// SaveBatch 批量保存
func (ms *MongoStorage) SaveBatch(_ context.Context, items map[string]interface{}) error {
	return ms.withRetry(func(c *mgo.Collection) error {
		bulk := c.Bulk()
		for id, state := range items {
			data, err := json.Marshal(state)
			if err != nil {
				return err
			}
			bulk.Upsert(
				bson.M{"_id": id},
				bson.M{"$set": bson.M{
					"state":      string(data),
					"updated_at": time.Now(),
				}},
			)
		}
		_, err := bulk.Run()
		return err
	})
}

// LoadBatch 批量加载
func (ms *MongoStorage) LoadBatch(_ context.Context, ids []string, factory func() interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	err := ms.withRetry(func(c *mgo.Collection) error {
		var docs []mongoDocument
		err := c.Find(bson.M{"_id": bson.M{"$in": ids}}).All(&docs)
		if err != nil {
			return err
		}

		for _, doc := range docs {
			target := factory()
			if err := json.Unmarshal([]byte(doc.State), target); err != nil {
				return fmt.Errorf("unmarshal %s: %w", doc.ID, err)
			}
			result[doc.ID] = target
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// EnsureIndex 创建索引
func (ms *MongoStorage) EnsureIndex(keys ...string) error {
	return ms.withRetry(func(c *mgo.Collection) error {
		return c.EnsureIndex(mgo.Index{Key: keys})
	})
}
