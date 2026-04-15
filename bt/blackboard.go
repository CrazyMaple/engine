package bt

// Blackboard 行为树黑板，节点间共享数据的键值存储
type Blackboard struct {
	data map[string]interface{}
}

// NewBlackboard 创建黑板
func NewBlackboard() *Blackboard {
	return &Blackboard{data: make(map[string]interface{})}
}

// Set 设置键值
func (bb *Blackboard) Set(key string, value interface{}) {
	bb.data[key] = value
}

// Get 获取值，不存在返回 nil
func (bb *Blackboard) Get(key string) interface{} {
	return bb.data[key]
}

// GetInt 获取整数值
func (bb *Blackboard) GetInt(key string) (int, bool) {
	v, ok := bb.data[key]
	if !ok {
		return 0, false
	}
	i, ok := v.(int)
	return i, ok
}

// GetFloat64 获取浮点值
func (bb *Blackboard) GetFloat64(key string) (float64, bool) {
	v, ok := bb.data[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

// GetString 获取字符串值
func (bb *Blackboard) GetString(key string) (string, bool) {
	v, ok := bb.data[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// GetBool 获取布尔值
func (bb *Blackboard) GetBool(key string) (bool, bool) {
	v, ok := bb.data[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// Has 检查键是否存在
func (bb *Blackboard) Has(key string) bool {
	_, ok := bb.data[key]
	return ok
}

// Delete 删除键
func (bb *Blackboard) Delete(key string) {
	delete(bb.data, key)
}

// Clear 清空所有数据
func (bb *Blackboard) Clear() {
	bb.data = make(map[string]interface{})
}

// Keys 返回所有键
func (bb *Blackboard) Keys() []string {
	keys := make([]string, 0, len(bb.data))
	for k := range bb.data {
		keys = append(keys, k)
	}
	return keys
}
