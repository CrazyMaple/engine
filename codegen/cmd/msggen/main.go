package main

import (
	"flag"
	"fmt"
	"os"

	"engine/codegen"
)

func main() {
	input := flag.String("input", "", "输入 Go 源文件（包含消息结构体定义）")
	proto := flag.String("proto", "", "输入 .proto 文件（与 -input 互斥）")
	output := flag.String("output", "", "输出 Go 文件路径")
	pkg := flag.String("pkg", "main", "生成代码的包名")
	tsOutput := flag.String("ts", "", "可选：TypeScript 类型定义输出路径")
	sdkOutput := flag.String("sdk", "", "可选：TypeScript 完整 SDK 输出路径（含 WebSocket 连接管理）")
	csOutput := flag.String("cs", "", "可选：C# 类型定义输出路径（Unity 客户端）")
	csNamespace := flag.String("cs-ns", "GameMessages", "C# 命名空间")
	docOutput := flag.String("doc", "", "可选：Markdown API 文档输出路径")
	registryOutput := flag.String("registry", "", "可选：TypeRegistry 注册代码输出路径")
	flag.Parse()

	if *input == "" && *proto == "" {
		fmt.Fprintln(os.Stderr, "Usage: msggen -input=messages.go|-proto=messages.proto -output=messages_gen.go [-pkg=mygame] [-ts=types.ts] [-sdk=sdk.ts] [-cs=Messages.cs] [-cs-ns=GameNS] [-doc=api.md] [-registry=registry_gen.go]")
		os.Exit(1)
	}
	if *input != "" && *proto != "" {
		fmt.Fprintln(os.Stderr, "错误: -input 和 -proto 不能同时指定")
		os.Exit(1)
	}

	var msgs []codegen.MessageDef
	var err error
	if *proto != "" {
		msgs, err = codegen.ParseProtoFile(*proto)
	} else {
		msgs, err = codegen.ParseFile(*input)
	}
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

	// 生成 TypeScript 类型定义（仅接口 + MessageIDs）
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

	// 生成 TypeScript 完整 SDK
	if *sdkOutput != "" {
		code, err := codegen.GenerateTSSDK(msgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 TypeScript SDK 失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*sdkOutput, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("TypeScript SDK 已生成: %s\n", *sdkOutput)
	}

	// 生成 C# 类型定义
	if *csOutput != "" {
		code, err := codegen.GenerateCSharp(msgs, *csNamespace)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 C# 代码失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*csOutput, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("C# 类型已生成: %s\n", *csOutput)
	}

	// 生成 Markdown API 文档
	if *docOutput != "" {
		code, err := codegen.GenerateMarkdownDoc(msgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 API 文档失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*docOutput, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("API 文档已生成: %s\n", *docOutput)
	}

	// 生成 TypeRegistry 注册代码
	if *registryOutput != "" {
		code, err := codegen.GenerateTypeRegistry(msgs, *pkg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 TypeRegistry 代码失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*registryOutput, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("TypeRegistry 注册代码已生成: %s\n", *registryOutput)
	}
}
