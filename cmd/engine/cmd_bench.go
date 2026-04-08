package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

func cmdBench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	pkg := fs.String("pkg", "./...", "测试包路径模式")
	count := fs.Int("count", 1, "基准测试运行次数")
	output := fs.String("output", "", "报告输出文件（空则输出到终端）")
	fs.Parse(args)

	fmt.Printf("运行基准测试: %s (count=%d)\n", *pkg, *count)

	cmd := exec.Command("go", "test", *pkg,
		"-bench=.", "-benchmem",
		fmt.Sprintf("-count=%d", *count),
		"-run=^$", // 不运行普通测试
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// 部分测试可能失败，但 bench 输出仍有效
		fmt.Printf("警告: 部分测试可能失败: %v\n", err)
	}

	results := parseBenchOutput(string(out))

	report := formatBenchReport(results)

	if *output != "" {
		if err := os.WriteFile(*output, []byte(report), 0644); err != nil {
			return fmt.Errorf("写入报告失败: %w", err)
		}
		fmt.Printf("报告已保存: %s\n", *output)
	} else {
		fmt.Println(report)
	}

	return nil
}

type benchResult struct {
	Name     string
	NsPerOp  float64
	BPerOp   int64
	AllocsOp int64
}

func parseBenchOutput(output string) []benchResult {
	var results []benchResult
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		r := benchResult{Name: fields[0]}

		for i := 2; i < len(fields); i++ {
			if i+1 < len(fields) {
				switch fields[i+1] {
				case "ns/op":
					r.NsPerOp, _ = strconv.ParseFloat(fields[i], 64)
				case "B/op":
					r.BPerOp, _ = strconv.ParseInt(fields[i], 10, 64)
				case "allocs/op":
					r.AllocsOp, _ = strconv.ParseInt(fields[i], 10, 64)
				}
			}
		}

		if r.NsPerOp > 0 {
			results = append(results, r)
		}
	}
	return results
}

func formatBenchReport(results []benchResult) string {
	var sb strings.Builder

	sb.WriteString("=== 基准测试报告 ===\n")
	sb.WriteString(fmt.Sprintf("生成时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	if len(results) == 0 {
		sb.WriteString("未找到基准测试结果\n")
		return sb.String()
	}

	// 按 ns/op 排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].NsPerOp < results[j].NsPerOp
	})

	sb.WriteString(fmt.Sprintf("%-60s %12s %10s %12s\n", "测试名称", "耗时/操作", "内存/操作", "分配次数/操作"))
	sb.WriteString(strings.Repeat("-", 98) + "\n")

	for _, r := range results {
		nsStr := formatDuration(r.NsPerOp)
		sb.WriteString(fmt.Sprintf("%-60s %12s %8d B %12d\n", r.Name, nsStr, r.BPerOp, r.AllocsOp))
	}

	sb.WriteString(fmt.Sprintf("\n共 %d 个基准测试\n", len(results)))
	return sb.String()
}

func formatDuration(ns float64) string {
	switch {
	case ns >= 1e9:
		return fmt.Sprintf("%.2f s", ns/1e9)
	case ns >= 1e6:
		return fmt.Sprintf("%.2f ms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.2f us", ns/1e3)
	default:
		return fmt.Sprintf("%.1f ns", ns)
	}
}
