package actor

import (
	"fmt"
	"sync/atomic"
)

// PID 是Actor的进程标识
type PID struct {
	Address string // 节点地址，本地为空，远程为"host:port"
	Id      string // Actor唯一标识
	p       Process
}

var pidCounter uint64

// NewPID 创建新的PID
func NewPID(address, id string) *PID {
	return &PID{
		Address: address,
		Id:      id,
	}
}

// NewLocalPID 创建本地PID
func NewLocalPID(id string) *PID {
	return &PID{
		Address: "",
		Id:      id,
	}
}

// GeneratePID 生成唯一PID
func GeneratePID() *PID {
	id := atomic.AddUint64(&pidCounter, 1)
	return NewLocalPID(fmt.Sprintf("$%d", id))
}

// String 返回PID的字符串表示
func (pid *PID) String() string {
	if pid.Address == "" {
		return pid.Id
	}
	return fmt.Sprintf("%s/%s", pid.Address, pid.Id)
}

// IsLocal 判断是否为本地PID
func (pid *PID) IsLocal() bool {
	return pid.Address == ""
}

// Equal 判断两个PID是否相等
func (pid *PID) Equal(other *PID) bool {
	if pid == nil || other == nil {
		return pid == other
	}
	return pid.Address == other.Address && pid.Id == other.Id
}
