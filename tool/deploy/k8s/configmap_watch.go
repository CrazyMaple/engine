package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gamelib/config"
	"engine/log"
)

// ConfigMapWatchConfig ConfigMap 热重载配置
type ConfigMapWatchConfig struct {
	// MountPath ConfigMap 挂载目录（如 "/etc/engine/config"）
	MountPath string
	// Manager 配置管理器
	Manager *config.Manager
	// PollInterval 轮询间隔（默认 5s）
	PollInterval time.Duration
	// FileFilter 文件过滤函数（可选，nil 则接受所有文件）
	FileFilter func(name string) bool
}

// StartConfigMapWatch 扫描 ConfigMap 挂载目录，将配置文件注册到 Manager 并启动热重载
// K8s ConfigMap 挂载使用 symlink 原子更新，修改时 ModTime 会变化，
// 因此 config.Manager 的 ModTime 轮询机制可以直接兼容。
func StartConfigMapWatch(cfg ConfigMapWatchConfig) error {
	if cfg.MountPath == "" {
		return fmt.Errorf("configmap mount path is empty")
	}
	if cfg.Manager == nil {
		return fmt.Errorf("config manager is nil")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}

	entries, err := os.ReadDir(cfg.MountPath)
	if err != nil {
		return fmt.Errorf("read configmap dir %s: %w", cfg.MountPath, err)
	}

	registered := 0
	for _, entry := range entries {
		name := entry.Name()
		// 跳过隐藏文件和 K8s 自动生成的 symlink 目录
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "..") {
			continue
		}
		if entry.IsDir() {
			continue
		}
		// 应用文件过滤
		if cfg.FileFilter != nil && !cfg.FileFilter(name) {
			continue
		}

		fullPath := filepath.Join(cfg.MountPath, name)
		ext := strings.ToLower(filepath.Ext(name))

		switch ext {
		case ".json":
			// JSON 配置：注册为空 target，用户应在注册后自行绑定结构体
			cfg.Manager.RegisterJSON(fullPath, nil, func() {
				log.Info("[configmap] reloaded %s", fullPath)
			})
			registered++
		default:
			// 其他格式（tsv/csv 等）：用户需自行通过 Manager.RegisterRecordFile 注册
			log.Info("[configmap] skipping unsupported file: %s", fullPath)
		}
	}

	log.Info("[configmap] registered %d config files from %s", registered, cfg.MountPath)

	// 启动热重载轮询
	cfg.Manager.StartWatch(cfg.PollInterval)
	return nil
}
