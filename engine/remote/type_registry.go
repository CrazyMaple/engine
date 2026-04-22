package remote

import (
	"encoding/json"
	"reflect"
	"sync"

	"engine/codec"
	engerr "engine/errors"
)

// TypeRegistry 消息类型注册表，支持跨节点消息的类型化反序列化
type TypeRegistry struct {
	nameToType map[string]reflect.Type
	typeToName map[reflect.Type]string
	mu         sync.RWMutex
}

// NewTypeRegistry 创建类型注册表
func NewTypeRegistry() *TypeRegistry {
	return &TypeRegistry{
		nameToType: make(map[string]reflect.Type),
		typeToName: make(map[reflect.Type]string),
	}
}

// Register 注册消息类型（传入指针或值均可）
func (r *TypeRegistry) Register(msg interface{}) {
	t := reflect.TypeOf(msg)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	name := t.PkgPath() + "." + t.Name()

	r.mu.Lock()
	defer r.mu.Unlock()

	r.nameToType[name] = t
	r.typeToName[t] = name
}

// RegisterName 以指定名称注册消息类型
func (r *TypeRegistry) RegisterName(name string, msg interface{}) {
	t := reflect.TypeOf(msg)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.nameToType[name] = t
	r.typeToName[t] = name
}

// GetTypeName 获取消息的类型名称
func (r *TypeRegistry) GetTypeName(msg interface{}) (string, bool) {
	t := reflect.TypeOf(msg)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	name, ok := r.typeToName[t]
	return name, ok
}

// Deserialize 根据类型名称反序列化 JSON 数据
func (r *TypeRegistry) Deserialize(typeName string, data []byte) (interface{}, error) {
	r.mu.RLock()
	t, ok := r.nameToType[typeName]
	r.mu.RUnlock()

	if !ok {
		return nil, &engerr.CodecError{Op: "deserialize", TypeName: typeName, Cause: engerr.ErrNotFound}
	}

	ptr := reflect.New(t).Interface()
	if err := json.Unmarshal(data, ptr); err != nil {
		return nil, &engerr.CodecError{Op: "deserialize", TypeName: typeName, Cause: err}
	}
	return ptr, nil
}

// DeserializeWith 使用指定 Codec 反序列化消息
func (r *TypeRegistry) DeserializeWith(typeName string, data []byte, c codec.Codec) (interface{}, error) {
	if c == nil {
		return r.Deserialize(typeName, data)
	}

	r.mu.RLock()
	_, ok := r.nameToType[typeName]
	r.mu.RUnlock()

	if !ok {
		return nil, &engerr.CodecError{Op: "deserialize", TypeName: typeName, Cause: engerr.ErrNotFound}
	}

	decoded, err := c.Decode(data)
	if err != nil {
		return nil, &engerr.CodecError{Op: "deserialize", TypeName: typeName, Cause: err}
	}
	return decoded, nil
}

// 全局默认类型注册表
var defaultTypeRegistry = NewTypeRegistry()

// DefaultTypeRegistry 返回全局默认类型注册表
func DefaultTypeRegistry() *TypeRegistry {
	return defaultTypeRegistry
}

// RegisterType 注册消息类型到全局注册表
func RegisterType(msg interface{}) {
	defaultTypeRegistry.Register(msg)
}

// RegisterTypeName 以指定名称注册消息类型到全局注册表
func RegisterTypeName(name string, msg interface{}) {
	defaultTypeRegistry.RegisterName(name, msg)
}
