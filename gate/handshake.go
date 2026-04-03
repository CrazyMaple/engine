package gate

import "encoding/json"

const (
	handshakeType    = "__handshake__"
	handshakeAckType = "__handshake_ack__"
)

// HandshakeRequest 客户端握手请求
type HandshakeRequest struct {
	Type              string `json:"type"`               // 固定值 "__handshake__"
	ProtocolVersion   int    `json:"protocol_version"`   // 客户端期望的协议版本
	ClientSDK         string `json:"client_sdk"`         // 客户端 SDK 标识，如 "ts-1.0.0"
	SupportedVersions []int  `json:"supported_versions"` // 客户端支持的版本列表
}

// HandshakeResponse 服务端握手响应
type HandshakeResponse struct {
	Type            string `json:"type"`             // 固定值 "__handshake_ack__"
	ProtocolVersion int    `json:"protocol_version"` // 协商后的版本
	ServerVersion   string `json:"server_version"`   // 服务端版本标识
	Status          string `json:"status"`           // "ok" 或 "version_mismatch"
	Message         string `json:"message,omitempty"`
}

// VersionNegotiator 版本协商器
type VersionNegotiator struct {
	MinVersion    int    // 服务端支持的最低协议版本
	MaxVersion    int    // 服务端支持的最高协议版本（当前版本）
	ServerVersion string // 服务端版本标识字符串
}

// NewVersionNegotiator 创建版本协商器
func NewVersionNegotiator(minVersion, maxVersion int, serverVersion string) *VersionNegotiator {
	return &VersionNegotiator{
		MinVersion:    minVersion,
		MaxVersion:    maxVersion,
		ServerVersion: serverVersion,
	}
}

// Negotiate 协商协议版本：从客户端支持的版本列表与服务端范围取交集，选最高版本
func (vn *VersionNegotiator) Negotiate(req *HandshakeRequest) *HandshakeResponse {
	resp := &HandshakeResponse{
		Type:          handshakeAckType,
		ServerVersion: vn.ServerVersion,
	}

	// 从客户端支持的版本中找出服务端也支持的最高版本
	bestVersion := -1
	for _, v := range req.SupportedVersions {
		if v >= vn.MinVersion && v <= vn.MaxVersion && v > bestVersion {
			bestVersion = v
		}
	}

	// 如果客户端未提供 SupportedVersions，则直接使用 ProtocolVersion
	if bestVersion < 0 && req.ProtocolVersion >= vn.MinVersion && req.ProtocolVersion <= vn.MaxVersion {
		bestVersion = req.ProtocolVersion
	}

	if bestVersion < 0 {
		resp.Status = "version_mismatch"
		resp.ProtocolVersion = vn.MaxVersion
		resp.Message = "no compatible protocol version found"
		return resp
	}

	resp.Status = "ok"
	resp.ProtocolVersion = bestVersion
	return resp
}

// isHandshakeRequest 检测原始数据是否为握手请求
func isHandshakeRequest(data []byte) bool {
	// 快速前缀检测避免完整 JSON 解析
	if len(data) < 20 {
		return false
	}
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return false
	}
	return peek.Type == handshakeType
}

// parseHandshakeRequest 解析握手请求
func parseHandshakeRequest(data []byte) (*HandshakeRequest, error) {
	var req HandshakeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}
