package stress

import (
	"time"
)

// AdaptiveTimeout 根据节点数动态计算超时
// 公式：base + perNode*nodeCount，每个节点引入额外等待，避免大集群假阴性
func AdaptiveTimeout(nodeCount int, base time.Duration, perNode time.Duration) time.Duration {
	if nodeCount <= 0 {
		return base
	}
	return base + perNode*time.Duration(nodeCount)
}

// Retry 简单重试封装：在失败时重试 n 次，每次间隔 interval。
// 返回 true 表示某次执行通过；false 表示全部失败。
func Retry(n int, interval time.Duration, fn func() bool) bool {
	if n <= 0 {
		n = 1
	}
	for i := 0; i < n; i++ {
		if fn() {
			return true
		}
		if i < n-1 {
			time.Sleep(interval)
		}
	}
	return false
}
