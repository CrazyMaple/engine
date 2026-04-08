package proto

// 核心消息 Go 实现
// 每个消息实现 encoding.BinaryMarshaler / BinaryUnmarshaler 接口
// 用于与 ProtobufCodec 配合使用

// 消息 ID 分配
const (
	// Remote 系列 1-10
	MsgIDPID                uint16 = 1
	MsgIDRemoteMessage      uint16 = 2
	MsgIDRemoteMessageBatch uint16 = 3

	// System 系列 11-20
	MsgIDStarted    uint16 = 11
	MsgIDStopping   uint16 = 12
	MsgIDStopped    uint16 = 13
	MsgIDRestarting uint16 = 14
	MsgIDWatch      uint16 = 15
	MsgIDUnwatch    uint16 = 16
	MsgIDTerminated uint16 = 17

	// Cluster 系列 21-40
	MsgIDMember            uint16 = 21
	MsgIDMemberGossipState uint16 = 22
	MsgIDGossipState       uint16 = 23
	MsgIDGossipRequest     uint16 = 24
	MsgIDGossipResponse    uint16 = 25
	MsgIDClusterTopology   uint16 = 26
)

// === Remote Messages ===

// ProtoPID Actor 进程标识（Proto 版本）
type ProtoPID struct {
	Address string
	Id      string
}

func (p *ProtoPID) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, len(p.Address)+len(p.Id)+8)
	buf = encodeString(buf, p.Address)
	buf = encodeString(buf, p.Id)
	return buf, nil
}

func (p *ProtoPID) UnmarshalBinary(data []byte) error {
	var offset int
	addr, n, err := decodeString(data)
	if err != nil {
		return err
	}
	p.Address = addr
	offset += n

	id, _, err := decodeString(data[offset:])
	if err != nil {
		return err
	}
	p.Id = id
	return nil
}

// ProtoRemoteMessage 远程消息（Proto 版本）
type ProtoRemoteMessage struct {
	Target          *ProtoPID
	Sender          *ProtoPID
	Payload         []byte // 序列化后的消息体
	MsgType         int32  // 0=User, 1=System
	TypeName        string
	ProtocolVersion int32
}

func (m *ProtoRemoteMessage) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, 128)

	// Target
	if m.Target != nil {
		targetData, _ := m.Target.MarshalBinary()
		buf = append(buf, 1) // has target
		buf = encodeBytes(buf, targetData)
	} else {
		buf = append(buf, 0)
	}

	// Sender
	if m.Sender != nil {
		senderData, _ := m.Sender.MarshalBinary()
		buf = append(buf, 1) // has sender
		buf = encodeBytes(buf, senderData)
	} else {
		buf = append(buf, 0)
	}

	// Payload
	buf = encodeBytes(buf, m.Payload)

	// MsgType
	buf = encodeInt32(buf, m.MsgType)

	// TypeName
	buf = encodeString(buf, m.TypeName)

	// ProtocolVersion
	buf = encodeInt32(buf, m.ProtocolVersion)

	return buf, nil
}

func (m *ProtoRemoteMessage) UnmarshalBinary(data []byte) error {
	offset := 0

	// Target
	if data[offset] == 1 {
		offset++
		targetData, n, err := decodeBytes(data[offset:])
		if err != nil {
			return err
		}
		offset += n
		m.Target = &ProtoPID{}
		if err := m.Target.UnmarshalBinary(targetData); err != nil {
			return err
		}
	} else {
		offset++
	}

	// Sender
	if data[offset] == 1 {
		offset++
		senderData, n, err := decodeBytes(data[offset:])
		if err != nil {
			return err
		}
		offset += n
		m.Sender = &ProtoPID{}
		if err := m.Sender.UnmarshalBinary(senderData); err != nil {
			return err
		}
	} else {
		offset++
	}

	// Payload
	payload, n, err := decodeBytes(data[offset:])
	if err != nil {
		return err
	}
	m.Payload = payload
	offset += n

	// MsgType
	msgType, n, err := decodeInt32(data[offset:])
	if err != nil {
		return err
	}
	m.MsgType = msgType
	offset += n

	// TypeName
	typeName, n, err := decodeString(data[offset:])
	if err != nil {
		return err
	}
	m.TypeName = typeName
	offset += n

	// ProtocolVersion
	pv, _, err := decodeInt32(data[offset:])
	if err != nil {
		return err
	}
	m.ProtocolVersion = pv

	return nil
}

// ProtoRemoteMessageBatch 批量远程消息（Proto 版本）
type ProtoRemoteMessageBatch struct {
	Messages []*ProtoRemoteMessage
}

func (b *ProtoRemoteMessageBatch) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, 256)
	buf = encodeVarint(buf, uint64(len(b.Messages)))
	for _, msg := range b.Messages {
		data, err := msg.MarshalBinary()
		if err != nil {
			return nil, err
		}
		buf = encodeBytes(buf, data)
	}
	return buf, nil
}

func (b *ProtoRemoteMessageBatch) UnmarshalBinary(data []byte) error {
	count, offset, err := decodeVarint(data)
	if err != nil {
		return err
	}
	b.Messages = make([]*ProtoRemoteMessage, 0, count)
	for i := uint64(0); i < count; i++ {
		msgData, n, err := decodeBytes(data[offset:])
		if err != nil {
			return err
		}
		offset += n
		msg := &ProtoRemoteMessage{}
		if err := msg.UnmarshalBinary(msgData); err != nil {
			return err
		}
		b.Messages = append(b.Messages, msg)
	}
	return nil
}

// === System Messages ===

// ProtoStarted Actor 启动完成
type ProtoStarted struct{}

func (m *ProtoStarted) MarshalBinary() ([]byte, error)       { return []byte{}, nil }
func (m *ProtoStarted) UnmarshalBinary(data []byte) error     { return nil }

// ProtoStopping Actor 正在停止
type ProtoStopping struct{}

func (m *ProtoStopping) MarshalBinary() ([]byte, error)       { return []byte{}, nil }
func (m *ProtoStopping) UnmarshalBinary(data []byte) error     { return nil }

// ProtoStopped Actor 已停止
type ProtoStopped struct{}

func (m *ProtoStopped) MarshalBinary() ([]byte, error)       { return []byte{}, nil }
func (m *ProtoStopped) UnmarshalBinary(data []byte) error     { return nil }

// ProtoRestarting Actor 正在重启
type ProtoRestarting struct{}

func (m *ProtoRestarting) MarshalBinary() ([]byte, error)       { return []byte{}, nil }
func (m *ProtoRestarting) UnmarshalBinary(data []byte) error     { return nil }

// ProtoWatch 监视请求
type ProtoWatch struct {
	Watcher *ProtoPID
}

func (m *ProtoWatch) MarshalBinary() ([]byte, error) {
	if m.Watcher == nil {
		return []byte{0}, nil
	}
	data, _ := m.Watcher.MarshalBinary()
	buf := []byte{1}
	return append(buf, data...), nil
}

func (m *ProtoWatch) UnmarshalBinary(data []byte) error {
	if len(data) == 0 || data[0] == 0 {
		return nil
	}
	m.Watcher = &ProtoPID{}
	return m.Watcher.UnmarshalBinary(data[1:])
}

// ProtoUnwatch 取消监视请求
type ProtoUnwatch struct {
	Watcher *ProtoPID
}

func (m *ProtoUnwatch) MarshalBinary() ([]byte, error) {
	if m.Watcher == nil {
		return []byte{0}, nil
	}
	data, _ := m.Watcher.MarshalBinary()
	buf := []byte{1}
	return append(buf, data...), nil
}

func (m *ProtoUnwatch) UnmarshalBinary(data []byte) error {
	if len(data) == 0 || data[0] == 0 {
		return nil
	}
	m.Watcher = &ProtoPID{}
	return m.Watcher.UnmarshalBinary(data[1:])
}

// ProtoTerminated Actor 已终止通知
type ProtoTerminated struct {
	Who *ProtoPID
}

func (m *ProtoTerminated) MarshalBinary() ([]byte, error) {
	if m.Who == nil {
		return []byte{0}, nil
	}
	data, _ := m.Who.MarshalBinary()
	buf := []byte{1}
	return append(buf, data...), nil
}

func (m *ProtoTerminated) UnmarshalBinary(data []byte) error {
	if len(data) == 0 || data[0] == 0 {
		return nil
	}
	m.Who = &ProtoPID{}
	return m.Who.UnmarshalBinary(data[1:])
}

// === Cluster Messages ===

// ProtoMemberStatus 成员状态
type ProtoMemberStatus int32

const (
	ProtoMemberAlive   ProtoMemberStatus = 0
	ProtoMemberSuspect ProtoMemberStatus = 1
	ProtoMemberDead    ProtoMemberStatus = 2
	ProtoMemberLeft    ProtoMemberStatus = 3
)

// ProtoMember 集群成员
type ProtoMember struct {
	Address string
	Id      string
	Kinds   []string
	Status  ProtoMemberStatus
	Seq     uint64
}

func (m *ProtoMember) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, 64)
	buf = encodeString(buf, m.Address)
	buf = encodeString(buf, m.Id)
	buf = encodeStringSlice(buf, m.Kinds)
	buf = encodeInt32(buf, int32(m.Status))
	buf = encodeUint64(buf, m.Seq)
	return buf, nil
}

func (m *ProtoMember) UnmarshalBinary(data []byte) error {
	offset := 0

	addr, n, err := decodeString(data[offset:])
	if err != nil {
		return err
	}
	m.Address = addr
	offset += n

	id, n, err := decodeString(data[offset:])
	if err != nil {
		return err
	}
	m.Id = id
	offset += n

	kinds, n, err := decodeStringSlice(data[offset:])
	if err != nil {
		return err
	}
	m.Kinds = kinds
	offset += n

	status, n, err := decodeInt32(data[offset:])
	if err != nil {
		return err
	}
	m.Status = ProtoMemberStatus(status)
	offset += n

	seq, _, err := decodeUint64(data[offset:])
	if err != nil {
		return err
	}
	m.Seq = seq
	return nil
}

// ProtoMemberGossipState 成员 Gossip 状态
type ProtoMemberGossipState struct {
	Address string
	Id      string
	Kinds   []string
	Status  ProtoMemberStatus
	Seq     uint64
}

func (m *ProtoMemberGossipState) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, 64)
	buf = encodeString(buf, m.Address)
	buf = encodeString(buf, m.Id)
	buf = encodeStringSlice(buf, m.Kinds)
	buf = encodeInt32(buf, int32(m.Status))
	buf = encodeUint64(buf, m.Seq)
	return buf, nil
}

func (m *ProtoMemberGossipState) UnmarshalBinary(data []byte) error {
	offset := 0

	addr, n, err := decodeString(data[offset:])
	if err != nil {
		return err
	}
	m.Address = addr
	offset += n

	id, n, err := decodeString(data[offset:])
	if err != nil {
		return err
	}
	m.Id = id
	offset += n

	kinds, n, err := decodeStringSlice(data[offset:])
	if err != nil {
		return err
	}
	m.Kinds = kinds
	offset += n

	status, n, err := decodeInt32(data[offset:])
	if err != nil {
		return err
	}
	m.Status = ProtoMemberStatus(status)
	offset += n

	seq, _, err := decodeUint64(data[offset:])
	if err != nil {
		return err
	}
	m.Seq = seq
	return nil
}

// ProtoGossipState Gossip 状态（CRDT）
type ProtoGossipState struct {
	Members map[string]*ProtoMemberGossipState
}

func (s *ProtoGossipState) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, 256)
	buf = encodeVarint(buf, uint64(len(s.Members)))
	for key, member := range s.Members {
		buf = encodeString(buf, key)
		data, err := member.MarshalBinary()
		if err != nil {
			return nil, err
		}
		buf = encodeBytes(buf, data)
	}
	return buf, nil
}

func (s *ProtoGossipState) UnmarshalBinary(data []byte) error {
	count, offset, err := decodeVarint(data)
	if err != nil {
		return err
	}
	s.Members = make(map[string]*ProtoMemberGossipState, count)
	for i := uint64(0); i < count; i++ {
		key, n, err := decodeString(data[offset:])
		if err != nil {
			return err
		}
		offset += n

		memberData, n, err := decodeBytes(data[offset:])
		if err != nil {
			return err
		}
		offset += n

		member := &ProtoMemberGossipState{}
		if err := member.UnmarshalBinary(memberData); err != nil {
			return err
		}
		s.Members[key] = member
	}
	return nil
}

// ProtoGossipRequest Gossip 请求
type ProtoGossipRequest struct {
	ClusterName string
	State       *ProtoGossipState
}

func (r *ProtoGossipRequest) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, 128)
	buf = encodeString(buf, r.ClusterName)
	if r.State != nil {
		stateData, err := r.State.MarshalBinary()
		if err != nil {
			return nil, err
		}
		buf = append(buf, 1) // has state
		buf = encodeBytes(buf, stateData)
	} else {
		buf = append(buf, 0)
	}
	return buf, nil
}

func (r *ProtoGossipRequest) UnmarshalBinary(data []byte) error {
	name, offset, err := decodeString(data)
	if err != nil {
		return err
	}
	r.ClusterName = name

	if data[offset] == 1 {
		offset++
		stateData, _, err := decodeBytes(data[offset:])
		if err != nil {
			return err
		}
		r.State = &ProtoGossipState{}
		return r.State.UnmarshalBinary(stateData)
	}
	return nil
}

// ProtoGossipResponse Gossip 响应
type ProtoGossipResponse struct {
	State *ProtoGossipState
}

func (r *ProtoGossipResponse) MarshalBinary() ([]byte, error) {
	if r.State == nil {
		return []byte{0}, nil
	}
	stateData, err := r.State.MarshalBinary()
	if err != nil {
		return nil, err
	}
	buf := []byte{1}
	return append(buf, stateData...), nil
}

func (r *ProtoGossipResponse) UnmarshalBinary(data []byte) error {
	if len(data) == 0 || data[0] == 0 {
		return nil
	}
	r.State = &ProtoGossipState{}
	return r.State.UnmarshalBinary(data[1:])
}

// ProtoClusterTopologyEvent 集群拓扑变更事件
type ProtoClusterTopologyEvent struct {
	Members []*ProtoMember
	Joined  []*ProtoMember
	Left    []*ProtoMember
}

func (e *ProtoClusterTopologyEvent) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, 256)
	buf = encodeMemberSlice(buf, e.Members)
	buf = encodeMemberSlice(buf, e.Joined)
	buf = encodeMemberSlice(buf, e.Left)
	return buf, nil
}

func (e *ProtoClusterTopologyEvent) UnmarshalBinary(data []byte) error {
	offset := 0

	members, n, err := decodeMemberSlice(data[offset:])
	if err != nil {
		return err
	}
	e.Members = members
	offset += n

	joined, n, err := decodeMemberSlice(data[offset:])
	if err != nil {
		return err
	}
	e.Joined = joined
	offset += n

	left, _, err := decodeMemberSlice(data[offset:])
	if err != nil {
		return err
	}
	e.Left = left
	return nil
}

func encodeMemberSlice(buf []byte, members []*ProtoMember) []byte {
	buf = encodeVarint(buf, uint64(len(members)))
	for _, m := range members {
		data, _ := m.MarshalBinary()
		buf = encodeBytes(buf, data)
	}
	return buf
}

func decodeMemberSlice(data []byte) ([]*ProtoMember, int, error) {
	count, offset, err := decodeVarint(data)
	if err != nil {
		return nil, 0, err
	}
	result := make([]*ProtoMember, 0, count)
	for i := uint64(0); i < count; i++ {
		memberData, n, err := decodeBytes(data[offset:])
		if err != nil {
			return nil, 0, err
		}
		offset += n
		m := &ProtoMember{}
		if err := m.UnmarshalBinary(memberData); err != nil {
			return nil, 0, err
		}
		result = append(result, m)
	}
	return result, offset, nil
}
