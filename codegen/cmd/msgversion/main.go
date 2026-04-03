package main

import (
	"flag"
	"fmt"
	"os"

	"engine/codegen"
)

func main() {
	oldFile := flag.String("old", "", "旧版本 Go 源文件")
	newFile := flag.String("new", "", "新版本 Go 源文件")
	manifestPath := flag.String("manifest", "", "版本清单 JSON 文件路径")
	checkOnly := flag.Bool("check", false, "仅检查兼容性，不更新清单（不兼容时退出码 1）")
	version := flag.Int("version", 0, "新协议版本号（更新清单时必填）")
	flag.Parse()

	if *oldFile == "" || *newFile == "" {
		fmt.Fprintln(os.Stderr, "Usage: msgversion -old=v1.go -new=v2.go [-manifest=manifest.json] [-check] [-version=2]")
		os.Exit(1)
	}

	oldMsgs, err := codegen.ParseFile(*oldFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析旧版本失败: %v\n", err)
		os.Exit(1)
	}

	newMsgs, err := codegen.ParseFile(*newFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析新版本失败: %v\n", err)
		os.Exit(1)
	}

	report := codegen.CheckCompatibility(oldMsgs, newMsgs)

	// 输出报告
	if len(report.Additions) > 0 {
		fmt.Println("--- Additions ---")
		for _, a := range report.Additions {
			fmt.Printf("  + %s\n", a)
		}
	}
	if len(report.Warnings) > 0 {
		fmt.Println("--- Warnings ---")
		for _, w := range report.Warnings {
			fmt.Printf("  ~ %s\n", w)
		}
	}
	if len(report.BreakingChanges) > 0 {
		fmt.Println("--- Breaking Changes ---")
		for _, b := range report.BreakingChanges {
			fmt.Printf("  ! %s\n", b)
		}
	}

	if report.Compatible {
		fmt.Println("\nResult: COMPATIBLE")
	} else {
		fmt.Println("\nResult: INCOMPATIBLE")
	}

	if *checkOnly {
		if !report.Compatible {
			os.Exit(1)
		}
		return
	}

	// 更新清单
	if *manifestPath != "" {
		if *version <= 0 {
			fmt.Fprintln(os.Stderr, "更新清单需要指定 -version 参数")
			os.Exit(1)
		}
		if err := codegen.UpdateManifest(*manifestPath, newMsgs, *version); err != nil {
			fmt.Fprintf(os.Stderr, "更新清单失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("版本清单已更新: %s (version=%d)\n", *manifestPath, *version)
	}
}
