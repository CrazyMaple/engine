package syncx

import (
	"encoding/binary"
	"errors"
	"math"
	"sort"
)

// DeltaSchema 描述实体状态字段元数据
//
// 通过 Register 为每个字段名分配一个 0..63 的位图位。位图位是确定性的
// （同一字段名总是映射到同一位），编码端和解码端只需注册相同字段集合即可。
type DeltaSchema struct {
	byName  map[string]uint8
	byIndex []string
}

// NewDeltaSchema 创建空 schema
func NewDeltaSchema() *DeltaSchema {
	return &DeltaSchema{byName: make(map[string]uint8)}
}

// Register 注册一个字段名，返回其位图位索引
func (s *DeltaSchema) Register(name string) uint8 {
	if idx, ok := s.byName[name]; ok {
		return idx
	}
	if len(s.byIndex) >= 64 {
		panic("syncx: DeltaSchema 单实体最多支持 64 个字段")
	}
	idx := uint8(len(s.byIndex))
	s.byName[name] = idx
	s.byIndex = append(s.byIndex, name)
	return idx
}

// FieldName 通过位索引返回字段名
func (s *DeltaSchema) FieldName(idx uint8) (string, bool) {
	if int(idx) >= len(s.byIndex) {
		return "", false
	}
	return s.byIndex[idx], true
}

// FieldCount 已注册字段数
func (s *DeltaSchema) FieldCount() int { return len(s.byIndex) }

// EntityDelta 单实体差分
type EntityDelta struct {
	EntityID  string
	Bitmap    uint64                 // 脏字段位图
	Fields    map[string]interface{} // 仅包含变化的字段值；nil 表示该字段被删除
	IsNew     bool
	IsRemoved bool
}

// FrameDelta 帧级差分
type FrameDelta struct {
	FrameNum  uint64
	Entities  []EntityDelta
	Snapshot  bool // true 表示这是一个完整快照（接收端应先 Reset）
}

// DeltaEncoder 服务端差分编码器
//
// 内部维护"上一帧已发送的状态"快照；每次 Encode 计算与新状态的差异。
type DeltaEncoder struct {
	schema   *DeltaSchema
	snapshot map[string]map[string]interface{}
}

// NewDeltaEncoder 创建编码器
func NewDeltaEncoder(schema *DeltaSchema) *DeltaEncoder {
	if schema == nil {
		schema = NewDeltaSchema()
	}
	return &DeltaEncoder{
		schema:   schema,
		snapshot: make(map[string]map[string]interface{}),
	}
}

// Schema 返回内部 schema（编码端可继续注册新字段）
func (e *DeltaEncoder) Schema() *DeltaSchema { return e.schema }

// Encode 计算与上一快照的差分；同时更新内部快照供下次比较
//
// 字段语义：
//   - 旧实体出现新键 / 旧值不等 → 视为变化
//   - 旧实体丢失某键 → 编码为 nil 值表示删除
//   - 完全新出现的实体 → IsNew=true，全字段编码
//   - 完全消失的实体 → IsRemoved=true
func (e *DeltaEncoder) Encode(frameNum uint64, state map[string]map[string]interface{}) *FrameDelta {
	fd := &FrameDelta{FrameNum: frameNum}
	seen := make(map[string]bool, len(state))

	for entityID, fields := range state {
		seen[entityID] = true
		prev, exists := e.snapshot[entityID]
		if !exists {
			ed := EntityDelta{EntityID: entityID, IsNew: true, Fields: make(map[string]interface{})}
			for name, v := range fields {
				idx := e.schema.Register(name)
				ed.Bitmap |= uint64(1) << idx
				ed.Fields[name] = v
			}
			fd.Entities = append(fd.Entities, ed)
		} else {
			ed := EntityDelta{EntityID: entityID, Fields: make(map[string]interface{})}
			for name, v := range fields {
				if pv, ok := prev[name]; !ok || pv != v {
					idx := e.schema.Register(name)
					ed.Bitmap |= uint64(1) << idx
					ed.Fields[name] = v
				}
			}
			for name := range prev {
				if _, ok := fields[name]; !ok {
					idx := e.schema.Register(name)
					ed.Bitmap |= uint64(1) << idx
					ed.Fields[name] = nil
				}
			}
			if ed.Bitmap != 0 {
				fd.Entities = append(fd.Entities, ed)
			}
		}
		cp := make(map[string]interface{}, len(fields))
		for k, v := range fields {
			cp[k] = v
		}
		e.snapshot[entityID] = cp
	}

	for entityID := range e.snapshot {
		if !seen[entityID] {
			fd.Entities = append(fd.Entities, EntityDelta{EntityID: entityID, IsRemoved: true})
			delete(e.snapshot, entityID)
		}
	}

	sort.Slice(fd.Entities, func(i, j int) bool {
		return fd.Entities[i].EntityID < fd.Entities[j].EntityID
	})
	return fd
}

// Snapshot 返回当前完整状态作为完整快照（用于新加入玩家或周期性强制全量同步）
func (e *DeltaEncoder) Snapshot(frameNum uint64) *FrameDelta {
	fd := &FrameDelta{FrameNum: frameNum, Snapshot: true}
	for entityID, fields := range e.snapshot {
		ed := EntityDelta{EntityID: entityID, IsNew: true, Fields: make(map[string]interface{})}
		for name, v := range fields {
			idx := e.schema.Register(name)
			ed.Bitmap |= uint64(1) << idx
			ed.Fields[name] = v
		}
		fd.Entities = append(fd.Entities, ed)
	}
	sort.Slice(fd.Entities, func(i, j int) bool {
		return fd.Entities[i].EntityID < fd.Entities[j].EntityID
	})
	return fd
}

// Reset 清空内部快照（强制下次 Encode 全量编码）
func (e *DeltaEncoder) Reset() {
	e.snapshot = make(map[string]map[string]interface{})
}

// DeltaDecoder 客户端差分解码器
type DeltaDecoder struct {
	schema *DeltaSchema
	state  map[string]map[string]interface{}
}

// NewDeltaDecoder 创建解码器
func NewDeltaDecoder(schema *DeltaSchema) *DeltaDecoder {
	if schema == nil {
		schema = NewDeltaSchema()
	}
	return &DeltaDecoder{
		schema: schema,
		state:  make(map[string]map[string]interface{}),
	}
}

// Apply 应用 FrameDelta 到本地状态
func (d *DeltaDecoder) Apply(fd *FrameDelta) {
	if fd == nil {
		return
	}
	if fd.Snapshot {
		d.state = make(map[string]map[string]interface{})
	}
	for _, ed := range fd.Entities {
		if ed.IsRemoved {
			delete(d.state, ed.EntityID)
			continue
		}
		entity, exists := d.state[ed.EntityID]
		if !exists || ed.IsNew {
			entity = make(map[string]interface{})
			d.state[ed.EntityID] = entity
		}
		for name, v := range ed.Fields {
			if v == nil {
				delete(entity, name)
			} else {
				entity[name] = v
			}
		}
	}
}

// State 返回当前完整状态副本
func (d *DeltaDecoder) State() map[string]map[string]interface{} {
	cp := make(map[string]map[string]interface{}, len(d.state))
	for k, fields := range d.state {
		m := make(map[string]interface{}, len(fields))
		for fk, fv := range fields {
			m[fk] = fv
		}
		cp[k] = m
	}
	return cp
}

// EntityState 返回单实体状态副本
func (d *DeltaDecoder) EntityState(entityID string) (map[string]interface{}, bool) {
	fields, ok := d.state[entityID]
	if !ok {
		return nil, false
	}
	m := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		m[k] = v
	}
	return m, true
}

// EntityCount 当前实体数
func (d *DeltaDecoder) EntityCount() int { return len(d.state) }

// Reset 清空状态
func (d *DeltaDecoder) Reset() {
	d.state = make(map[string]map[string]interface{})
}

// --- 二进制编解码（紧凑格式，用于网络传输节省带宽）---
//
// 仅支持基础类型：int64 / float64 / bool / string / nil。业务层若使用其他类型
// 应在编码前手动转为这些基础类型。位图压缩天然实现"仅传输变化字段"。
//
// 格式（小端）：
//   | frameNum(8) | flags(1) | entityCount(2) | [entity...] |
//   entity: | idLen(2) | id | flags(1) | bitmap(8) | fieldCount(1) | [field...] |
//   field:  | fieldIdx(1) | typeTag(1) | value... |
//   value:  按类型编码（int64=8B / float64=8B / bool=1B / string=2B+data / nil=0B）

const (
	deltaFlagSnapshot uint8 = 0x01

	entityFlagNew     uint8 = 0x01
	entityFlagRemoved uint8 = 0x02

	tagNil     uint8 = 0
	tagInt64   uint8 = 1
	tagFloat64 uint8 = 2
	tagBool    uint8 = 3
	tagString  uint8 = 4
)

// MarshalBinary 将 FrameDelta 编码为紧凑二进制
func MarshalDelta(fd *FrameDelta, schema *DeltaSchema) ([]byte, error) {
	if fd == nil {
		return nil, errors.New("syncx: nil FrameDelta")
	}
	if schema == nil {
		return nil, errors.New("syncx: schema required")
	}
	if len(fd.Entities) > 0xFFFF {
		return nil, errors.New("syncx: too many entities (>65535)")
	}
	buf := make([]byte, 0, 64+32*len(fd.Entities))
	tmp := make([]byte, 8)

	binary.LittleEndian.PutUint64(tmp, fd.FrameNum)
	buf = append(buf, tmp...)
	var flags uint8
	if fd.Snapshot {
		flags |= deltaFlagSnapshot
	}
	buf = append(buf, flags)
	binary.LittleEndian.PutUint16(tmp[:2], uint16(len(fd.Entities)))
	buf = append(buf, tmp[:2]...)

	for _, ed := range fd.Entities {
		if len(ed.EntityID) > 0xFFFF {
			return nil, errors.New("syncx: entity id too long")
		}
		binary.LittleEndian.PutUint16(tmp[:2], uint16(len(ed.EntityID)))
		buf = append(buf, tmp[:2]...)
		buf = append(buf, ed.EntityID...)
		var efl uint8
		if ed.IsNew {
			efl |= entityFlagNew
		}
		if ed.IsRemoved {
			efl |= entityFlagRemoved
		}
		buf = append(buf, efl)
		binary.LittleEndian.PutUint64(tmp, ed.Bitmap)
		buf = append(buf, tmp...)
		fieldCount := bitsSet(ed.Bitmap)
		if fieldCount > 0xFF {
			return nil, errors.New("syncx: too many fields per entity")
		}
		buf = append(buf, uint8(fieldCount))
		// 按位图位升序编码字段
		for bit := uint8(0); bit < 64; bit++ {
			if ed.Bitmap&(uint64(1)<<bit) == 0 {
				continue
			}
			name, ok := schema.FieldName(bit)
			if !ok {
				return nil, errors.New("syncx: bitmap bit references unknown field")
			}
			val, present := ed.Fields[name]
			buf = append(buf, bit)
			if !present || val == nil {
				buf = append(buf, tagNil)
				continue
			}
			var err error
			buf, err = appendValue(buf, val, tmp)
			if err != nil {
				return nil, err
			}
		}
	}
	return buf, nil
}

// UnmarshalDelta 从二进制数据反序列化 FrameDelta
func UnmarshalDelta(buf []byte, schema *DeltaSchema) (*FrameDelta, error) {
	if schema == nil {
		return nil, errors.New("syncx: schema required")
	}
	r := &reader{buf: buf}
	frameNum, err := r.uint64()
	if err != nil {
		return nil, err
	}
	flags, err := r.uint8()
	if err != nil {
		return nil, err
	}
	entityCount, err := r.uint16()
	if err != nil {
		return nil, err
	}
	fd := &FrameDelta{FrameNum: frameNum, Snapshot: flags&deltaFlagSnapshot != 0}
	fd.Entities = make([]EntityDelta, 0, entityCount)
	for i := 0; i < int(entityCount); i++ {
		idLen, err := r.uint16()
		if err != nil {
			return nil, err
		}
		idBytes, err := r.bytes(int(idLen))
		if err != nil {
			return nil, err
		}
		efl, err := r.uint8()
		if err != nil {
			return nil, err
		}
		bitmap, err := r.uint64()
		if err != nil {
			return nil, err
		}
		fieldCount, err := r.uint8()
		if err != nil {
			return nil, err
		}
		ed := EntityDelta{
			EntityID:  string(idBytes),
			Bitmap:    bitmap,
			IsNew:     efl&entityFlagNew != 0,
			IsRemoved: efl&entityFlagRemoved != 0,
			Fields:    make(map[string]interface{}, fieldCount),
		}
		for j := 0; j < int(fieldCount); j++ {
			bit, err := r.uint8()
			if err != nil {
				return nil, err
			}
			name, ok := schema.FieldName(bit)
			if !ok {
				return nil, errors.New("syncx: unknown field index")
			}
			tag, err := r.uint8()
			if err != nil {
				return nil, err
			}
			val, err := readValue(r, tag)
			if err != nil {
				return nil, err
			}
			ed.Fields[name] = val
		}
		fd.Entities = append(fd.Entities, ed)
	}
	return fd, nil
}

func appendValue(buf []byte, v interface{}, tmp []byte) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(buf, tagNil), nil
	case int:
		buf = append(buf, tagInt64)
		binary.LittleEndian.PutUint64(tmp, uint64(int64(x)))
		return append(buf, tmp...), nil
	case int64:
		buf = append(buf, tagInt64)
		binary.LittleEndian.PutUint64(tmp, uint64(x))
		return append(buf, tmp...), nil
	case float64:
		buf = append(buf, tagFloat64)
		binary.LittleEndian.PutUint64(tmp, math.Float64bits(x))
		return append(buf, tmp...), nil
	case bool:
		buf = append(buf, tagBool)
		if x {
			return append(buf, 1), nil
		}
		return append(buf, 0), nil
	case string:
		if len(x) > 0xFFFF {
			return nil, errors.New("syncx: string too long")
		}
		buf = append(buf, tagString)
		binary.LittleEndian.PutUint16(tmp[:2], uint16(len(x)))
		buf = append(buf, tmp[:2]...)
		return append(buf, x...), nil
	default:
		return nil, errors.New("syncx: unsupported delta value type")
	}
}

func readValue(r *reader, tag uint8) (interface{}, error) {
	switch tag {
	case tagNil:
		return nil, nil
	case tagInt64:
		v, err := r.uint64()
		if err != nil {
			return nil, err
		}
		return int64(v), nil
	case tagFloat64:
		v, err := r.uint64()
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(v), nil
	case tagBool:
		b, err := r.uint8()
		if err != nil {
			return nil, err
		}
		return b != 0, nil
	case tagString:
		l, err := r.uint16()
		if err != nil {
			return nil, err
		}
		data, err := r.bytes(int(l))
		if err != nil {
			return nil, err
		}
		return string(data), nil
	default:
		return nil, errors.New("syncx: unknown type tag")
	}
}

func bitsSet(x uint64) int {
	count := 0
	for x != 0 {
		x &= x - 1
		count++
	}
	return count
}

type reader struct {
	buf []byte
	pos int
}

func (r *reader) need(n int) error {
	if r.pos+n > len(r.buf) {
		return errors.New("syncx: unexpected end of delta buffer")
	}
	return nil
}

func (r *reader) uint8() (uint8, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	v := r.buf[r.pos]
	r.pos++
	return v, nil
}

func (r *reader) uint16() (uint16, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint16(r.buf[r.pos:])
	r.pos += 2
	return v, nil
}

func (r *reader) uint64() (uint64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if err := r.need(n); err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, r.buf[r.pos:r.pos+n])
	r.pos += n
	return out, nil
}
