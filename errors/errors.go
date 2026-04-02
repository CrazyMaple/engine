package errors

import (
	"errors"
	"fmt"
)

// 哨兵错误 — 用 errors.Is 判断
var (
	ErrNotFound     = errors.New("not found")
	ErrTimeout      = errors.New("timeout")
	ErrClosed       = errors.New("closed")
	ErrUnauthorized = errors.New("unauthorized")
)

// ConnectError Remote 连接错误
type ConnectError struct {
	Address string
	Cause   error
}

func (e *ConnectError) Error() string {
	return fmt.Sprintf("connect %s: %v", e.Address, e.Cause)
}

func (e *ConnectError) Unwrap() error { return e.Cause }

// TimeoutError 操作超时错误
type TimeoutError struct {
	Op    string
	Cause error
}

func (e *TimeoutError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s timeout: %v", e.Op, e.Cause)
	}
	return fmt.Sprintf("%s timeout", e.Op)
}

func (e *TimeoutError) Unwrap() error { return ErrTimeout }

func (e *TimeoutError) Is(target error) bool {
	return target == ErrTimeout
}

// AuthError 认证/签名验证错误
type AuthError struct {
	Reason string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("auth failed: %s", e.Reason)
}

func (e *AuthError) Is(target error) bool {
	return target == ErrUnauthorized
}

// ClusterError 集群操作错误
type ClusterError struct {
	Op    string
	Node  string
	Cause error
}

func (e *ClusterError) Error() string {
	if e.Node != "" {
		return fmt.Sprintf("cluster %s [%s]: %v", e.Op, e.Node, e.Cause)
	}
	return fmt.Sprintf("cluster %s: %v", e.Op, e.Cause)
}

func (e *ClusterError) Unwrap() error { return e.Cause }

// CodecError 编解码错误
type CodecError struct {
	Op       string // "encode" 或 "decode"
	TypeName string
	Cause    error
}

func (e *CodecError) Error() string {
	if e.TypeName != "" {
		return fmt.Sprintf("codec %s [%s]: %v", e.Op, e.TypeName, e.Cause)
	}
	return fmt.Sprintf("codec %s: %v", e.Op, e.Cause)
}

func (e *CodecError) Unwrap() error { return e.Cause }
