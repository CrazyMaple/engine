package bench

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseBenchOutput 解析 `go test -bench=.` 的标准输出，提取 BenchResult 列表
// 支持 -benchmem 输出的 B/op 和 allocs/op 字段
//
// 典型行格式：
//   BenchmarkActorSend-8           	 1000000	      1234 ns/op	     256 B/op	       3 allocs/op
//   BenchmarkMailbox-8             	 500000	        2345 ns/op	   1.23 MB/s
func ParseBenchOutput(r io.Reader) ([]BenchResult, error) {
	scanner := bufio.NewScanner(r)
	// 放宽缓冲区，长基准名可能导致超限
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		results    []BenchResult
		currentPkg string
	)
	for scanner.Scan() {
		line := scanner.Text()

		// 包路径标记行（go test 输出在每个包测试完成后打印 ok/FAIL + 路径）
		if pkg := extractPackage(line); pkg != "" {
			currentPkg = pkg
			continue
		}

		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}

		res, err := parseBenchLine(line)
		if err != nil {
			// 单行失败不中断整体解析
			continue
		}
		res.Package = currentPkg
		results = append(results, res)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// parseBenchLine 解析一行基准输出
func parseBenchLine(line string) (BenchResult, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return BenchResult{}, fmt.Errorf("too few fields: %q", line)
	}

	name := trimBenchSuffix(fields[0])
	iters, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return BenchResult{}, fmt.Errorf("parse iterations: %w", err)
	}

	res := BenchResult{
		Name:       name,
		Iterations: iters,
	}

	// 成对扫描剩余字段：<value> <unit>
	for i := 2; i+1 < len(fields); i += 2 {
		val, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			continue
		}
		unit := fields[i+1]
		switch unit {
		case "ns/op":
			res.NsPerOp = val
		case "MB/s":
			res.MBPerSec = val
		case "B/op":
			res.BytesPerOp = int64(val)
		case "allocs/op":
			res.AllocsPerOp = int64(val)
		}
	}

	if res.NsPerOp == 0 {
		return BenchResult{}, fmt.Errorf("no ns/op in line: %q", line)
	}
	return res, nil
}

// trimBenchSuffix 去掉 go test 基准名后的 GOMAXPROCS 后缀
// "BenchmarkActorSend-8" → "BenchmarkActorSend"
func trimBenchSuffix(name string) string {
	if idx := strings.LastIndex(name, "-"); idx > 0 {
		if _, err := strconv.Atoi(name[idx+1:]); err == nil {
			return name[:idx]
		}
	}
	return name
}

// extractPackage 从 go test 输出中提取包名行
// 支持的格式：
//   ok  	engine/actor	1.234s
//   FAIL	engine/remote	2.345s
//   PASS
//   goos: linux / goarch: amd64 / pkg: engine/actor
func extractPackage(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "pkg:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "pkg:"))
	}
	if strings.HasPrefix(line, "ok  \t") || strings.HasPrefix(line, "ok\t") {
		line = strings.TrimPrefix(line, "ok  \t")
		line = strings.TrimPrefix(line, "ok\t")
		parts := strings.Fields(line)
		if len(parts) > 0 {
			return parts[0]
		}
	}
	if strings.HasPrefix(line, "FAIL\t") {
		parts := strings.Fields(strings.TrimPrefix(line, "FAIL\t"))
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return ""
}
