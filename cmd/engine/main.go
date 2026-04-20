package main

import (
	"fmt"
	"os"
)

var commands = map[string]func(args []string) error{
	"init":      cmdInit,
	"gen":       cmdGen,
	"run":       cmdRun,
	"dashboard": cmdDashboard,
	"bench":     cmdBench,
	"plugin":    cmdPlugin,
	"cluster":   cmdCluster,
	"migrate":   cmdMigrate,
	"audit":     cmdAudit,
	"doctor":    cmdDoctor,
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err := cmd(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`engine — 游戏引擎 CLI 工具

使用方法:
  engine <command> [flags]

命令:
  init        初始化项目脚手架
  gen         统一代码生成入口（消息注册 + SDK + Proto）
  run         带热重载的开发模式
  dashboard   独立启动 Dashboard 面板
  bench       运行全部基准测试并生成报告
  plugin      插件管理（install/remove/list/info）
  cluster     集群状态查看（status/nodes/health）
  migrate     Actor 迁移管理（actor/drain/status）
  audit       API 稳定性扫描 / CHANGELOG 生成（stability/changelog）
  doctor      环境自检（Go 版本 / 配置 / 端口 / 服务 / 磁盘）

使用 engine <command> -h 查看各命令帮助`)
}
