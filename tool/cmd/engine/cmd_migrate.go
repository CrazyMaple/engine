package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func cmdMigrate(args []string) error {
	if len(args) == 0 {
		return printMigrateUsage()
	}
	switch args[0] {
	case "actor":
		return migrateActor(args[1:])
	case "drain":
		return migrateDrain(args[1:])
	case "status":
		return migrateStatus(args[1:])
	default:
		return fmt.Errorf("未知子命令: %s", args[0])
	}
}

func printMigrateUsage() error {
	fmt.Println(`engine migrate — Actor 迁移管理

使用方法:
  engine migrate actor -id <actor_id> -to <target_node>   迁移指定 Actor 到目标节点
  engine migrate drain -node <node_addr>                   排空节点（迁移所有 Actor 到其他节点）
  engine migrate status                                    查看迁移状态`)
	return nil
}

// migrateActor 手动触发 Actor 迁移
func migrateActor(args []string) error {
	fs := flag.NewFlagSet("migrate actor", flag.ExitOnError)
	addr := fs.String("addr", "http://localhost:8080", "Dashboard 地址")
	actorID := fs.String("id", "", "要迁移的 Actor ID（必填）")
	targetNode := fs.String("to", "", "目标节点地址（必填）")
	fs.Parse(args)

	if *actorID == "" || *targetNode == "" {
		return fmt.Errorf("请指定 -id 和 -to 参数")
	}

	fmt.Printf("迁移 Actor %s → %s\n", *actorID, *targetNode)

	// 调用 Dashboard API 触发迁移
	payload, _ := json.Marshal(map[string]string{
		"actor_id":    *actorID,
		"target_node": *targetNode,
	})

	data, err := postAPI(*addr, "/api/migrate/actor", payload)
	if err != nil {
		return fmt.Errorf("迁移请求失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err == nil {
		if status, ok := result["status"]; ok {
			fmt.Printf("迁移状态: %v\n", status)
		}
		if msg, ok := result["message"]; ok {
			fmt.Printf("详情: %v\n", msg)
		}
	} else {
		fmt.Printf("响应: %s\n", string(data))
	}

	return nil
}

// migrateDrain 排空节点
func migrateDrain(args []string) error {
	fs := flag.NewFlagSet("migrate drain", flag.ExitOnError)
	addr := fs.String("addr", "http://localhost:8080", "Dashboard 地址")
	node := fs.String("node", "", "要排空的节点地址（必填）")
	fs.Parse(args)

	if *node == "" {
		return fmt.Errorf("请指定 -node 参数")
	}

	fmt.Printf("排空节点: %s\n", *node)
	fmt.Println("正在将所有 Actor 迁移到其他节点...")

	payload, _ := json.Marshal(map[string]string{
		"node": *node,
	})

	data, err := postAPI(*addr, "/api/migrate/drain", payload)
	if err != nil {
		return fmt.Errorf("排空请求失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err == nil {
		if migrated, ok := result["migrated_count"]; ok {
			fmt.Printf("已迁移 Actor 数: %v\n", migrated)
		}
		if status, ok := result["status"]; ok {
			fmt.Printf("状态: %v\n", status)
		}
	} else {
		fmt.Printf("响应: %s\n", string(data))
	}

	return nil
}

// migrateStatus 查看迁移状态
func migrateStatus(args []string) error {
	fs := flag.NewFlagSet("migrate status", flag.ExitOnError)
	addr := fs.String("addr", "http://localhost:8080", "Dashboard 地址")
	fs.Parse(args)

	data, err := fetchAPI(*addr, "/api/migrate/status")
	if err != nil {
		return fmt.Errorf("获取迁移状态失败: %w", err)
	}

	var status struct {
		Active []struct {
			ActorID    string `json:"actor_id"`
			FromNode   string `json:"from_node"`
			ToNode     string `json:"to_node"`
			State      string `json:"state"`
			StartedAt  string `json:"started_at"`
		} `json:"active_migrations"`
		Redirects int `json:"redirect_count"`
	}

	if err := json.Unmarshal(data, &status); err != nil {
		// API 可能不存在，输出原始响应
		fmt.Printf("响应: %s\n", string(data))
		return nil
	}

	if len(status.Active) == 0 {
		fmt.Println("当前无进行中的迁移")
	} else {
		fmt.Printf("进行中的迁移 (%d 个):\n", len(status.Active))
		for _, m := range status.Active {
			fmt.Printf("  %s: %s → %s [%s] (开始于 %s)\n",
				m.ActorID, m.FromNode, m.ToNode, m.State, m.StartedAt)
		}
	}
	fmt.Printf("活跃重定向数: %d\n", status.Redirects)

	return nil
}

// postAPI 发送 POST 请求
func postAPI(baseAddr, path string, body []byte) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(
		baseAddr+path,
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data := make([]byte, 0, 1024)
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}

	if resp.StatusCode >= 400 {
		return data, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}
