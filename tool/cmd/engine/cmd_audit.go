package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"tool/codegen"
)

// cmdAudit API 稳定性与变更日志审计
//
// 子命令：
//   stability   扫描代码库，输出每个公开符号的稳定性分级
//   changelog   从 git log 生成符合 Conventional Commits 的 CHANGELOG
func cmdAudit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("audit 子命令: stability | changelog")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "stability":
		return cmdAuditStability(rest)
	case "changelog":
		return cmdAuditChangelog(rest)
	default:
		return fmt.Errorf("未知子命令: %s", sub)
	}
}

func cmdAuditStability(args []string) error {
	fs := flag.NewFlagSet("audit stability", flag.ExitOnError)
	root := fs.String("root", ".", "扫描根目录")
	format := fs.String("format", "summary", "输出格式: summary | markdown | json")
	output := fs.String("output", "", "输出到文件（默认 stdout）")
	fs.Parse(args)

	report, err := codegen.ScanStability(*root, nil)
	if err != nil {
		return fmt.Errorf("扫描失败: %w", err)
	}

	var out string
	switch *format {
	case "summary":
		out = report.Summary()
	case "markdown":
		out = report.Markdown()
	case "json":
		data, err := marshalStabilityJSON(report)
		if err != nil {
			return err
		}
		out = string(data)
	default:
		return fmt.Errorf("未知格式: %s", *format)
	}

	if *output != "" {
		return os.WriteFile(*output, []byte(out), 0644)
	}
	fmt.Print(out)
	return nil
}

func cmdAuditChangelog(args []string) error {
	fs := flag.NewFlagSet("audit changelog", flag.ExitOnError)
	from := fs.String("from", "", "起点 ref（如上一个 tag）")
	to := fs.String("to", "HEAD", "终点 ref")
	version := fs.String("version", "", "版本号（标题用）")
	max := fs.Int("max", 200, "最大提交数（仅在 -from 为空时生效）")
	output := fs.String("output", "", "输出文件（默认 stdout）")
	fs.Parse(args)

	md, err := codegen.GenerateGitChangelog(*from, *to, *version, *max)
	if err != nil {
		return err
	}
	if *output != "" {
		return os.WriteFile(*output, []byte(md), 0644)
	}
	fmt.Print(md)
	return nil
}

// marshalStabilityJSON 以 JSON 形式输出
func marshalStabilityJSON(r *codegen.StabilityReport) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
