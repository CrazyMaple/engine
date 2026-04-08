package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	mainPkg := fs.String("main", ".", "主包路径")
	watchDir := fs.String("watch", ".", "监控目录")
	fs.Parse(args)

	fmt.Printf("开发模式: 监控 %s, 主包 %s\n", *watchDir, *mainPkg)

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var cmd *exec.Cmd
	tmpBin := filepath.Join(os.TempDir(), "engine-dev-"+fmt.Sprintf("%d", time.Now().UnixNano()))

	build := func() error {
		fmt.Println("[engine] 编译中...")
		out, err := exec.Command("go", "build", "-o", tmpBin, *mainPkg).CombinedOutput()
		if err != nil {
			fmt.Printf("[engine] 编译失败:\n%s\n", out)
			return err
		}
		fmt.Println("[engine] 编译成功")
		return nil
	}

	start := func() {
		cmd = exec.Command(tmpBin)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			fmt.Printf("[engine] 启动失败: %v\n", err)
			return
		}
		fmt.Printf("[engine] 进程已启动 (PID: %d)\n", cmd.Process.Pid)
	}

	stop := func() {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				cmd.Process.Kill()
			}
			cmd = nil
		}
	}

	// 初始构建和启动
	if err := build(); err != nil {
		return err
	}
	start()
	defer func() {
		stop()
		os.Remove(tmpBin)
	}()

	// 收集初始文件状态
	lastMod := collectModTimes(*watchDir)

	// 监控循环
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Println("\n[engine] 收到退出信号")
			return nil
		case <-ticker.C:
			newMod := collectModTimes(*watchDir)
			if hasChanges(lastMod, newMod) {
				fmt.Println("[engine] 检测到文件变更，重新编译...")
				stop()
				if err := build(); err == nil {
					start()
				}
				lastMod = newMod
			}
		}
	}
}

func collectModTimes(dir string) map[string]time.Time {
	mods := make(map[string]time.Time)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// 跳过隐藏目录和 vendor
			base := filepath.Base(path)
			if len(base) > 0 && base[0] == '.' || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
			mods[path] = info.ModTime()
		}
		return nil
	})
	return mods
}

func hasChanges(old, new map[string]time.Time) bool {
	if len(old) != len(new) {
		return true
	}
	for path, newTime := range new {
		if oldTime, ok := old[path]; !ok || !oldTime.Equal(newTime) {
			return true
		}
	}
	return false
}
