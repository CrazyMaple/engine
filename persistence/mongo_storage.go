package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

// MongoStorage MongoDB 存储后端
type MongoStorage struct {
	session    *mgo.Session
	db         string
	collection string
}

type mongoDocument struct {
	ID        string    `bson:"_id"`
	State     string    `bson:"state"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// NewMongoStorage 创建 MongoDB 存储
func NewMongoStorage(session *mgo.Session, db, collection string) *MongoStorage {
	return &MongoStorage{
		session:    session,
		db:         db,
		collection: collection,
	}
}

func (ms *MongoStorage) getCollection() *mgo.Collection {
	return ms.session.DB(ms.db).C(ms.collection)
}

func (ms *MongoStorage) Save(_ context.Context, id string, state interface{}) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	_, err = ms.getCollection().UpsertId(id, bson.M{
		"$set": bson.M{
			"state":      string(data),
			"updated_at": time.Now(),
		},
	})
	return err
}

func (ms *MongoStorage) Load(_ context.Context, id string, target interface{}) error {
	var doc mongoDocument
	if err := ms.getCollection().FindId(id).One(&doc); err != nil {
		if err == mgo.ErrNotFound {
			return fmt.Errorf("not found: %s", id)
		}
		return err
	}
	return json.Unmarshal([]byte(doc.State), target)
}

func (ms *MongoStorage) Delete(_ context.Context, id string) error {
	return ms.getCollection().RemoveId(id)
}
