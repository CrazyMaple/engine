package main

import (
	"flag"
	"fmt"
	"os"

	"engine/codegen"
)

func main() {
	input := flag.String("input", "", "输入 Go 源文件（包含消息结构体定义）")
	output := flag.String("output", "", "输出 Go 文件路径")
	pkg := flag.String("pkg", "main", "生成代码的包名")
	tsOutput := flag.String("ts", "", "可选：TypeScript 类型定义输出路径")
	flag.Parse()

	if *input == "" {
		fmt.Fprintln(os.Stderr, "Usage: msggen -input=messages.go -output=messages_gen.go [-pkg=mygame] [-ts=types.ts]")
		os.Exit(1)
	}

	msgs, err := codegen.ParseFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析失败: %v\n", err)
		os.Exit(1)
	}

	if len(msgs) == 0 {
		fmt.Fprintln(os.Stderr, "未找到 //msggen:message 标记的消息结构体")
		os.Exit(0)
	}

	fmt.Printf("发现 %d 个消息定义\n", len(msgs))

	// 生成 Go 代码
	if *output != "" {
		code, err := codegen.GenerateGo(msgs, *pkg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 Go 代码失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*output, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Go 代码已生成: %s\n", *output)
	}

	// 生成 TypeScript 代码
	if *tsOutput != "" {
		code, err := codegen.GenerateTS(msgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 TypeScript 失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*tsOutput, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("TypeScript 类型已生成: %s\n", *tsOutput)
	}
}
