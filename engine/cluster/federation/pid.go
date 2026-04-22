package federation

import (
	"fmt"
	"strings"

	"engine/actor"
)

// FederationScheme 联邦 PID 地址前缀
const FederationScheme = "cluster://"

// ParseFederatedPID 解析联邦 PID 地址
// 格式: "cluster://clusterID/actorPath"
// 返回 clusterID 和 actorPath
func ParseFederatedPID(address string) (clusterID string, actorPath string, err error) {
	if !IsFederated(address) {
		return "", "", fmt.Errorf("not a federated address: %s", address)
	}

	rest := address[len(FederationScheme):]
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return rest, "", nil // 只有 clusterID，没有 actor path
	}

	return rest[:idx], rest[idx+1:], nil
}

// FederatedPIDString 构建联邦 PID 地址字符串
func FederatedPIDString(clusterID, actorPath string) string {
	return FederationScheme + clusterID + "/" + actorPath
}

// IsFederated 检查地址是否使用联邦寻址方案
func IsFederated(address string) bool {
	return strings.HasPrefix(address, FederationScheme)
}

// NewFederatedPID 创建联邦 PID
func NewFederatedPID(clusterID, actorPath string) *actor.PID {
	return &actor.PID{
		Address: FederatedPIDString(clusterID, actorPath),
		Id:      actorPath,
	}
}
