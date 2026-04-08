package remote

import (
	"encoding/json"
	"fmt"

	"engine/codec"
	engerr "engine/errors"
)

// RemoteCodec Remote 层编解码器，封装 codec.Codec 接口
// 提供 Remote 层专用的信封序列化/反序列化方法
type RemoteCodec struct {
	innerCodec codec.Codec // nil = 使用 JSON 兜底
	codecType  string      // "json" / "protobuf" 等
}

// NewRemoteCodec 创建指定 Codec 的 RemoteCodec
func NewRemoteCodec(c codec.Codec, codecType string) *RemoteCodec {
	return &RemoteCodec{
		innerCodec: c,
		codecType:  codecType,
	}
}

// DefaultRemoteCodec 创建默认 JSON RemoteCodec（向后兼容）
func DefaultRemoteCodec() *RemoteCodec {
	return &RemoteCodec{
		innerCodec: nil,
		codecType:  "json",
	}
}

// CodecType 返回编解码类型标识
func (rc *RemoteCodec) CodecType() string {
	return rc.codecType
}

// InnerCodec 返回内部 Codec 实例（可能为 nil）
func (rc *RemoteCodec) InnerCodec() codec.Codec {
	return rc.innerCodec
}

// MarshalEnvelope 序列化 RemoteMessage 或 RemoteMessageBatch
func (rc *RemoteCodec) MarshalEnvelope(msg interface{}) ([]byte, error) {
	if rc.innerCodec != nil {
		data, err := rc.innerCodec.Encode(msg)
		if err != nil {
			return nil, &engerr.CodecError{Op: "marshal_envelope", TypeName: fmt.Sprintf("%T", msg), Cause: err}
		}
		return data, nil
	}
	// JSON 兜底
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, &engerr.CodecError{Op: "marshal_envelope", TypeName: fmt.Sprintf("%T", msg), Cause: err}
	}
	return data, nil
}

// UnmarshalEnvelope 反序列化远程消息，自动区分单条/批量
// 返回 *RemoteMessage 或 *RemoteMessageBatch
func (rc *RemoteCodec) UnmarshalEnvelope(data []byte) (isBatch bool, batch *RemoteMessageBatch, single *RemoteMessage, err error) {
	if rc.innerCodec != nil {
		// Codec 模式：尝试先解码为 batch，失败则解码为 single
		decoded, decErr := rc.innerCodec.Decode(data)
		if decErr != nil {
			return false, nil, nil, &engerr.CodecError{Op: "unmarshal_envelope", TypeName: "RemoteMessage", Cause: decErr}
		}
		switch v := decoded.(type) {
		case *RemoteMessageBatch:
			return true, v, nil, nil
		case *RemoteMessage:
			return false, nil, v, nil
		default:
			return false, nil, nil, &engerr.CodecError{Op: "unmarshal_envelope", TypeName: fmt.Sprintf("%T", decoded), Cause: fmt.Errorf("unexpected type")}
		}
	}

	// JSON 兜底：先尝试批量，再尝试单条
	var batchMsg RemoteMessageBatch
	if err := json.Unmarshal(data, &batchMsg); err == nil && len(batchMsg.Messages) > 0 {
		return true, &batchMsg, nil, nil
	}

	var remoteMsg RemoteMessage
	if err := json.Unmarshal(data, &remoteMsg); err != nil {
		return false, nil, nil, &engerr.CodecError{Op: "unmarshal_envelope", TypeName: "RemoteMessage", Cause: err}
	}
	return false, nil, &remoteMsg, nil
}

// MarshalPayload 序列化用户消息体
func (rc *RemoteCodec) MarshalPayload(msg interface{}) ([]byte, error) {
	if rc.innerCodec != nil {
		return rc.innerCodec.Encode(msg)
	}
	return json.Marshal(msg)
}

// UnmarshalPayload 结合 TypeRegistry 反序列化用户消息体
func (rc *RemoteCodec) UnmarshalPayload(typeName string, data []byte, registry *TypeRegistry) (interface{}, error) {
	if rc.innerCodec != nil {
		return registry.DeserializeWith(typeName, data, rc.innerCodec)
	}
	return registry.Deserialize(typeName, data)
}
