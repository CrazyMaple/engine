package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"gamelib/hotreload"
)

func cmdPlugin(args []string) error {
	if len(args) == 0 {
		return printPluginUsage()
	}

	switch args[0] {
	case "list":
		return pluginList(args[1:])
	case "install":
		return pluginInstall(args[1:])
	case "remove":
		return pluginRemove(args[1:])
	case "info":
		return pluginInfo(args[1:])
	default:
		return fmt.Errorf("未知子命令: %s", args[0])
	}
}

func printPluginUsage() error {
	fmt.Println(`engine plugin — 插件管理

使用方法:
  engine plugin list              列出所有已安装插件
  engine plugin install <path>    安装插件（从 plugin.yaml 所在目录）
  engine plugin remove <name>     卸载插件
  engine plugin info <path>       查看插件清单信息`)
	return nil
}

// pluginList 列出 plugins/ 目录下所有插件
func pluginList(args []string) error {
	fs := flag.NewFlagSet("plugin list", flag.ExitOnError)
	dir := fs.String("dir", "plugins", "插件目录")
	fs.Parse(args)

	entries, err := os.ReadDir(*dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("插件目录不存在:", *dir)
			fmt.Println("提示: 使用 engine plugin install <path> 安装插件")
			return nil
		}
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "名称\t版本\t描述\t依赖")
	fmt.Fprintln(w, "----\t----\t----\t----")

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(*dir, entry.Name(), "plugin.yaml")
		m, err := hotreload.LoadManifest(manifestPath)
		if err != nil {
			continue
		}

		deps := make([]string, 0, len(m.Dependencies))
		for _, d := range m.Dependencies {
			deps = append(deps, d.Name)
		}
		depStr := "-"
		if len(deps) > 0 {
			depStr = strings.Join(deps, ", ")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", m.Name, m.Version, m.Description, depStr)
		count++
	}
	w.Flush()

	if count == 0 {
		fmt.Println("（无已安装插件）")
	} else {
		fmt.Printf("\n共 %d 个插件\n", count)
	}
	return nil
}

// pluginInstall 安装插件（复制到 plugins/ 目录并验证清单）
func pluginInstall(args []string) error {
	fs := flag.NewFlagSet("plugin install", flag.ExitOnError)
	targetDir := fs.String("dir", "plugins", "插件安装目标目录")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return fmt.Errorf("请指定插件路径（包含 plugin.yaml 的目录）")
	}
	srcPath := fs.Arg(0)

	// 加载并验证清单
	manifestPath := filepath.Join(srcPath, "plugin.yaml")
	m, err := hotreload.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("无法加载插件清单: %w", err)
	}

	fmt.Printf("安装插件: %s v%s\n", m.Name, m.Version)
	if m.Description != "" {
		fmt.Printf("  描述: %s\n", m.Description)
	}

	// 检查依赖
	if len(m.Dependencies) > 0 {
		fmt.Println("  依赖:")
		resolver := hotreload.NewDependencyResolver()
		resolver.Add(m)

		// 加载已安装插件以检查依赖
		installed := loadInstalledManifests(*targetDir)
		for _, im := range installed {
			resolver.Add(im)
		}

		for _, dep := range m.Dependencies {
			found := false
			for _, im := range installed {
				if im.Name == dep.Name {
					found = true
					fmt.Printf("    - %s >= %s  [已安装: %s]\n", dep.Name, dep.MinVersion, im.Version)
					break
				}
			}
			if !found {
				fmt.Printf("    - %s >= %s  [未安装]\n", dep.Name, dep.MinVersion)
			}
		}
	}

	// 创建目标目录
	destDir := filepath.Join(*targetDir, m.Name)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 复制 plugin.yaml
	if err := copyFile(manifestPath, filepath.Join(destDir, "plugin.yaml")); err != nil {
		return fmt.Errorf("复制清单失败: %w", err)
	}

	// 复制 entrypoint（如果存在）
	if m.Entrypoint != "" {
		srcEntry := filepath.Join(srcPath, m.Entrypoint)
		if _, err := os.Stat(srcEntry); err == nil {
			destEntry := filepath.Join(destDir, m.Entrypoint)
			os.MkdirAll(filepath.Dir(destEntry), 0755)
			if err := copyFile(srcEntry, destEntry); err != nil {
				return fmt.Errorf("复制入口文件失败: %w", err)
			}
		}
	}

	fmt.Printf("插件 %s 安装成功 → %s\n", m.Name, destDir)
	return nil
}

// pluginRemove 卸载插件
func pluginRemove(args []string) error {
	fs := flag.NewFlagSet("plugin remove", flag.ExitOnError)
	dir := fs.String("dir", "plugins", "插件目录")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return fmt.Errorf("请指定要卸载的插件名称")
	}
	name := fs.Arg(0)

	pluginDir := filepath.Join(*dir, name)
	manifestPath := filepath.Join(pluginDir, "plugin.yaml")

	m, err := hotreload.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("插件 %q 未安装或清单无效: %w", name, err)
	}

	// 检查是否有其他插件依赖它
	installed := loadInstalledManifests(*dir)
	for _, im := range installed {
		if im.Name == name {
			continue
		}
		for _, dep := range im.Dependencies {
			if dep.Name == name {
				return fmt.Errorf("插件 %q 被 %q 依赖，请先卸载 %q", name, im.Name, im.Name)
			}
		}
	}

	fmt.Printf("卸载插件: %s v%s\n", m.Name, m.Version)
	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("删除插件目录失败: %w", err)
	}

	fmt.Printf("插件 %s 已卸载\n", name)
	return nil
}

// pluginInfo 查看插件详细信息
func pluginInfo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("请指定插件路径或名称")
	}

	path := args[0]
	// 尝试直接作为目录路径
	manifestPath := filepath.Join(path, "plugin.yaml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		// 尝试 plugins/<name>/plugin.yaml
		manifestPath = filepath.Join("plugins", path, "plugin.yaml")
	}

	m, err := hotreload.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("无法加载清单: %w", err)
	}

	fmt.Printf("插件名称: %s\n", m.Name)
	fmt.Printf("版本:     %s\n", m.Version)
	fmt.Printf("描述:     %s\n", m.Description)
	fmt.Printf("作者:     %s\n", m.Author)
	fmt.Printf("最低引擎: %s\n", m.EngineMinVer)
	fmt.Printf("入口文件: %s\n", m.Entrypoint)

	if len(m.Dependencies) > 0 {
		fmt.Println("依赖:")
		for _, d := range m.Dependencies {
			fmt.Printf("  - %s >= %s\n", d.Name, d.MinVersion)
		}
	}
	if len(m.Config) > 0 {
		fmt.Println("默认配置:")
		for k, v := range m.Config {
			fmt.Printf("  %s = %s\n", k, v)
		}
	}

	return nil
}

// --- 辅助函数 ---

func loadInstalledManifests(dir string) []*hotreload.PluginManifest {
	var result []*hotreload.PluginManifest
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := hotreload.LoadManifest(filepath.Join(dir, e.Name(), "plugin.yaml"))
		if err != nil {
			continue
		}
		result = append(result, m)
	}
	return result
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
