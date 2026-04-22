package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
	"time"
)

func cmdCluster(args []string) error {
	if len(args) == 0 {
		return printClusterUsage()
	}
	switch args[0] {
	case "status":
		return clusterStatus(args[1:])
	case "nodes":
		return clusterNodes(args[1:])
	case "health":
		return clusterHealth(args[1:])
	default:
		return fmt.Errorf("未知子命令: %s", args[0])
	}
}

func printClusterUsage() error {
	fmt.Println(`engine cluster — 集群状态查看

使用方法:
  engine cluster status          查看集群概要状态
  engine cluster nodes           列出所有节点
  engine cluster health          集群健康检查`)
	return nil
}

// clusterStatus 查看集群概况（连接 Dashboard API）
func clusterStatus(args []string) error {
	fs := flag.NewFlagSet("cluster status", flag.ExitOnError)
	addr := fs.String("addr", "http://localhost:8080", "Dashboard 地址")
	fs.Parse(args)

	// 获取系统信息
	sysData, err := fetchAPI(*addr, "/api/system")
	if err != nil {
		return fmt.Errorf("无法连接 Dashboard: %w\n提示: 确保 Dashboard 已启动（engine dashboard）", err)
	}

	fmt.Println("=== 集群状态 ===")

	// 解析系统信息
	var sysInfo map[string]interface{}
	if err := json.Unmarshal(sysData, &sysInfo); err == nil {
		if addr, ok := sysInfo["address"]; ok {
			fmt.Printf("本机地址:   %v\n", addr)
		}
		if actors, ok := sysInfo["actor_count"]; ok {
			fmt.Printf("Actor 数量: %v\n", actors)
		}
		if uptime, ok := sysInfo["uptime"]; ok {
			fmt.Printf("运行时间:   %v\n", uptime)
		}
	}

	// 获取集群信息
	clusterData, err := fetchAPI(*addr, "/api/cluster")
	if err == nil {
		var clusterInfo map[string]interface{}
		if err := json.Unmarshal(clusterData, &clusterInfo); err == nil {
			if nodes, ok := clusterInfo["nodes"]; ok {
				if nodeList, ok := nodes.([]interface{}); ok {
					fmt.Printf("集群节点:   %d 个\n", len(nodeList))
				}
			}
			if status, ok := clusterInfo["status"]; ok {
				fmt.Printf("集群状态:   %v\n", status)
			}
		}
	}

	// 获取运行时指标
	runtimeData, err := fetchAPI(*addr, "/api/runtime")
	if err == nil {
		var rt map[string]interface{}
		if err := json.Unmarshal(runtimeData, &rt); err == nil {
			if goroutines, ok := rt["goroutines"]; ok {
				fmt.Printf("Goroutine:  %v\n", goroutines)
			}
			if memMB, ok := rt["heap_alloc_mb"]; ok {
				fmt.Printf("堆内存:     %.1f MB\n", toFloat(memMB))
			}
		}
	}

	return nil
}

// clusterNodes 列出集群节点
func clusterNodes(args []string) error {
	fs := flag.NewFlagSet("cluster nodes", flag.ExitOnError)
	addr := fs.String("addr", "http://localhost:8080", "Dashboard 地址")
	fs.Parse(args)

	data, err := fetchAPI(*addr, "/api/cluster")
	if err != nil {
		return fmt.Errorf("无法获取集群信息: %w", err)
	}

	var info struct {
		Nodes []struct {
			ID      string `json:"id"`
			Address string `json:"address"`
			State   string `json:"state"`
			Kinds   []string `json:"kinds"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if len(info.Nodes) == 0 {
		fmt.Println("当前无集群节点（单机模式）")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\t地址\t状态\tKinds")
	fmt.Fprintln(w, "--\t----\t----\t-----")
	for _, n := range info.Nodes {
		kinds := "-"
		if len(n.Kinds) > 0 {
			kinds = fmt.Sprintf("%v", n.Kinds)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n.ID, n.Address, n.State, kinds)
	}
	w.Flush()
	fmt.Printf("\n共 %d 个节点\n", len(info.Nodes))
	return nil
}

// clusterHealth 健康检查
func clusterHealth(args []string) error {
	fs := flag.NewFlagSet("cluster health", flag.ExitOnError)
	addr := fs.String("addr", "http://localhost:8080", "Dashboard 地址")
	fs.Parse(args)

	// Liveness check
	_, liveErr := fetchAPI(*addr, "/healthz")
	if liveErr != nil {
		fmt.Println("Liveness:  FAIL")
		return fmt.Errorf("健康检查失败: %w", liveErr)
	}
	fmt.Println("Liveness:  OK")

	// Readiness check
	_, readyErr := fetchAPI(*addr, "/readyz")
	if readyErr != nil {
		fmt.Println("Readiness: FAIL")
	} else {
		fmt.Println("Readiness: OK")
	}

	return nil
}

// --- 辅助函数 ---

func fetchAPI(baseAddr, path string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseAddr + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
