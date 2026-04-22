package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gamelib/config"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	name := fs.String("name", "", "项目名称（默认使用目录名）")
	dir := fs.String("dir", ".", "输出目录")
	fs.Parse(args)

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}

	projectName := *name
	if projectName == "" {
		projectName = filepath.Base(absDir)
	}

	fmt.Printf("初始化项目: %s -> %s\n", projectName, absDir)

	// 创建目录结构
	dirs := []string{
		absDir,
		filepath.Join(absDir, "config"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("创建目录 %s: %w", d, err)
		}
	}

	// go.mod
	if err := writeIfNotExist(filepath.Join(absDir, "go.mod"), fmt.Sprintf(`module %s

go 1.24

require engine v0.0.0
`, projectName)); err != nil {
		return err
	}

	// main.go
	if err := writeIfNotExist(filepath.Join(absDir, "main.go"), `package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"engine/actor"
	"engine/log"
)

func main() {
	log.SetLogger(log.NewTextLogger(os.Stdout))
	log.SetLevel(log.LevelInfo)

	system := actor.NewActorSystem()
	log.Info("ActorSystem started, address: %s", system.Address)

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("\nShutting down...")
}
`); err != nil {
		return err
	}

	// messages.go
	if err := writeIfNotExist(filepath.Join(absDir, "messages.go"), `package main

//msggen:message
// LoginRequest 登录请求
type LoginRequest struct {
	Username string `+"`"+`json:"username"`+"`"+`
	Password string `+"`"+`json:"password"`+"`"+`
}

//msggen:message
// LoginResponse 登录响应
type LoginResponse struct {
	Success bool   `+"`"+`json:"success"`+"`"+`
	Token   string `+"`"+`json:"token"`+"`"+`
	UserID  int64  `+"`"+`json:"user_id"`+"`"+`
}
`); err != nil {
		return err
	}

	// Makefile
	if err := writeIfNotExist(filepath.Join(absDir, "Makefile"), fmt.Sprintf(`.PHONY: gen test run build

gen:
	engine gen -input=messages.go -output=messages_gen.go -pkg=%s

test:
	go test ./...

run:
	engine run

build:
	go build -o %s .
`, projectName, projectName)); err != nil {
		return err
	}

	// 示例配置
	if err := writeIfNotExist(filepath.Join(absDir, "config", "server.json"), `{
  "addr": ":8080",
  "max_conn": 1000,
  "log_level": "info"
}
`); err != nil {
		return err
	}

	// engine.yaml 模板，与 engine run --config engine.yaml 对齐
	if err := writeIfNotExist(filepath.Join(absDir, "engine.yaml"), string(config.GenerateTemplate())); err != nil {
		return err
	}

	fmt.Println("项目脚手架已生成:")
	fmt.Println("  go.mod")
	fmt.Println("  main.go")
	fmt.Println("  messages.go")
	fmt.Println("  Makefile")
	fmt.Println("  config/server.json")
	fmt.Println("  engine.yaml  (用于 engine run --config engine.yaml)")
	fmt.Printf("\n下一步:\n  cd %s && go mod tidy\n", *dir)
	return nil
}

func writeIfNotExist(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  [跳过] %s (已存在)\n", filepath.Base(path))
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入 %s: %w", path, err)
	}
	return nil
}
