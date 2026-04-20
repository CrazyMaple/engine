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

	"engine/config"
	"engine/log"
)

// cmdRun 两种模式：
//   1. 开发模式（默认）：监控 .go 文件变更，自动 rebuild + 重启子进程
//   2. 启动模式（传入 --config 或 -c）：以 engine.yaml 为入口加载并在当前进程启动整套引擎
//
// SIGHUP 触发配置热重载（仅启动模式下有效）
func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	mainPkg := fs.String("main", ".", "主包路径（开发模式）")
	watchDir := fs.String("watch", ".", "监控目录（开发模式）")
	cfgPath := fs.String("config", "", "engine.yaml 路径；非空则进入启动模式")
	cfgShort := fs.String("c", "", "-config 简写")
	fs.Parse(args)

	path := *cfgPath
	if path == "" {
		path = *cfgShort
	}
	if path != "" {
		return cmdRunEngine(path)
	}
	return cmdRunDevMode(*mainPkg, *watchDir)
}

// cmdRunEngine 以 engine.yaml 启动整套引擎
func cmdRunEngine(cfgPath string) error {
	cfg, err := config.LoadEngineConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load engine config %q: %w", cfgPath, err)
	}

	runtime := newRuntime(cfgPath, cfg)
	if err := runtime.Start(); err != nil {
		runtime.Stop()
		return fmt.Errorf("start engine: %w", err)
	}
	defer runtime.Stop()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for s := range sigCh {
		switch s {
		case syscall.SIGHUP:
			if err := runtime.Reload(); err != nil {
				log.Error("reload engine config failed: %v", err)
			} else {
				log.Info("engine config reloaded from %s", cfgPath)
			}
		case syscall.SIGINT, syscall.SIGTERM:
			log.Info("engine stopping on signal %s", s)
			return nil
		}
	}
	return nil
}

// cmdRunDevMode 开发模式：文件变更 → rebuild → restart
func cmdRunDevMode(mainPkg, watchDir string) error {
	fmt.Printf("开发模式: 监控 %s, 主包 %s\n", watchDir, mainPkg)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var cmd *exec.Cmd
	tmpBin := filepath.Join(os.TempDir(), "engine-dev-"+fmt.Sprintf("%d", time.Now().UnixNano()))

	build := func() error {
		fmt.Println("[engine] 编译中...")
		out, err := exec.Command("go", "build", "-o", tmpBin, mainPkg).CombinedOutput()
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

	if err := build(); err != nil {
		return err
	}
	start()
	defer func() {
		stop()
		os.Remove(tmpBin)
	}()

	lastMod := collectModTimes(watchDir)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Println("\n[engine] 收到退出信号")
			return nil
		case <-ticker.C:
			newMod := collectModTimes(watchDir)
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
