package proto

import (
	"engine/codec"
	"engine/remote"
)

// RegisterAllMessages 注册所有 Proto 消息到 BinaryCodec
func RegisterAllMessages(c *codec.BinaryCodec) {
	// Remote 系列
	c.Register(&ProtoPID{}, MsgIDPID)
	c.Register(&ProtoRemoteMessage{}, MsgIDRemoteMessage)
	c.Register(&ProtoRemoteMessageBatch{}, MsgIDRemoteMessageBatch)

	// System 系列
	c.Register(&ProtoStarted{}, MsgIDStarted)
	c.Register(&ProtoStopping{}, MsgIDStopping)
	c.Register(&ProtoStopped{}, MsgIDStopped)
	c.Register(&ProtoRestarting{}, MsgIDRestarting)
	c.Register(&ProtoWatch{}, MsgIDWatch)
	c.Register(&ProtoUnwatch{}, MsgIDUnwatch)
	c.Register(&ProtoTerminated{}, MsgIDTerminated)

	// Cluster 系列
	c.Register(&ProtoMember{}, MsgIDMember)
	c.Register(&ProtoMemberGossipState{}, MsgIDMemberGossipState)
	c.Register(&ProtoGossipState{}, MsgIDGossipState)
	c.Register(&ProtoGossipRequest{}, MsgIDGossipRequest)
	c.Register(&ProtoGossipResponse{}, MsgIDGossipResponse)
	c.Register(&ProtoClusterTopologyEvent{}, MsgIDClusterTopology)
}

// RegisterAllTypes 注册所有 Proto 消息到 TypeRegistry（用于跨节点类型化反序列化）
func RegisterAllTypes(tr *remote.TypeRegistry) {
	// Remote 系列
	tr.Register(&ProtoPID{})
	tr.Register(&ProtoRemoteMessage{})
	tr.Register(&ProtoRemoteMessageBatch{})

	// System 系列
	tr.Register(&ProtoStarted{})
	tr.Register(&ProtoStopping{})
	tr.Register(&ProtoStopped{})
	tr.Register(&ProtoRestarting{})
	tr.Register(&ProtoWatch{})
	tr.Register(&ProtoUnwatch{})
	tr.Register(&ProtoTerminated{})

	// Cluster 系列
	tr.Register(&ProtoMember{})
	tr.Register(&ProtoMemberGossipState{})
	tr.Register(&ProtoGossipState{})
	tr.Register(&ProtoGossipRequest{})
	tr.Register(&ProtoGossipResponse{})
	tr.Register(&ProtoClusterTopologyEvent{})
}
