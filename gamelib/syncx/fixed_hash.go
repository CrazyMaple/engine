package syncx

import (
	"encoding/binary"
	"hash/crc32"

	"gamelib/fixedpoint"
)

// FixedHasher 定点数帧哈希计算器
// 替代浮点数哈希，确保跨平台一致性
type FixedHasher struct {
	h *crc32.Table
}

// NewFixedHasher 创建定点数哈希器
func NewFixedHasher() *FixedHasher {
	return &FixedHasher{
		h: crc32.MakeTable(crc32.IEEE),
	}
}

// HashInputs 使用定点数精度对帧输入计算哈希
// 将输入中的浮点数坐标/数值转为定点数后再哈希，保证跨平台一致
func (fh *FixedHasher) HashInputs(inputs []PlayerInput) uint32 {
	h := crc32.New(fh.h)
	buf := make([]byte, 4)
	for _, input := range inputs {
		h.Write([]byte(input.PlayerID))
		for _, action := range input.Actions {
			binary.LittleEndian.PutUint16(buf[:2], action.Type)
			h.Write(buf[:2])
			// 对数值类参数用定点数归一化后再哈希
			for _, v := range action.Data {
				switch val := v.(type) {
				case float64:
					fp := fixedpoint.FromFloat64(val)
					binary.LittleEndian.PutUint32(buf, uint32(fp.Raw()))
					h.Write(buf)
				case float32:
					fp := fixedpoint.FromFloat64(float64(val))
					binary.LittleEndian.PutUint32(buf, uint32(fp.Raw()))
					h.Write(buf)
				case int:
					binary.LittleEndian.PutUint32(buf, uint32(int32(val)))
					h.Write(buf)
				}
			}
		}
	}
	return h.Sum32()
}

// HashState 对状态 map 使用定点数精度计算哈希
func (fh *FixedHasher) HashState(state map[string]interface{}) uint32 {
	h := crc32.New(fh.h)
	buf := make([]byte, 4)
	for k, v := range state {
		h.Write([]byte(k))
		switch val := v.(type) {
		case float64:
			fp := fixedpoint.FromFloat64(val)
			binary.LittleEndian.PutUint32(buf, uint32(fp.Raw()))
			h.Write(buf)
		case float32:
			fp := fixedpoint.FromFloat64(float64(val))
			binary.LittleEndian.PutUint32(buf, uint32(fp.Raw()))
			h.Write(buf)
		case int:
			binary.LittleEndian.PutUint32(buf, uint32(int32(val)))
			h.Write(buf)
		}
	}
	return h.Sum32()
}
