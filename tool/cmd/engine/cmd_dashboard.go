package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"engine/actor"
	"tool/dashboard"
	"engine/log"
)

func cmdDashboard(args []string) error {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "HTTP 监听地址")
	fs.Parse(args)

	// 初始化日志
	log.SetLogger(log.NewTextLogger(os.Stdout))
	log.SetLevel(log.LevelInfo)

	// 创建最小 ActorSystem
	system := actor.NewActorSystem()

	// 创建 Dashboard
	d := dashboard.New(dashboard.Config{
		Addr:     *addr,
		System:   system,
		AuditLog: dashboard.NewAuditLog(),
	})

	if err := d.Start(); err != nil {
		return fmt.Errorf("启动 Dashboard 失败: %w", err)
	}
	fmt.Printf("Dashboard 已启动: http://localhost%s\n", *addr)

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\n正在关闭...")
	d.Stop()
	return nil
}
