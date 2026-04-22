package main

import (
	"flag"
	"fmt"
	"os"

	"tool/codegen"
)

func cmdGen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	input := fs.String("input", "", "输入 Go 源文件")
	proto := fs.String("proto", "", "输入 .proto 文件（与 -input 互斥）")
	output := fs.String("output", "", "输出 Go 文件路径")
	pkg := fs.String("pkg", "main", "生成代码的包名")
	tsOutput := fs.String("ts", "", "TypeScript 类型定义输出路径")
	sdkOutput := fs.String("sdk", "", "TypeScript SDK 输出路径")
	csOutput := fs.String("cs", "", "C# 类型定义输出路径")
	csNamespace := fs.String("cs-ns", "GameMessages", "C# 命名空间")
	docOutput := fs.String("doc", "", "Markdown API 文档输出路径")
	registryOutput := fs.String("registry", "", "TypeRegistry 注册代码输出路径")
	tsRPCOutput := fs.String("ts-rpc", "", "TypeScript RPC/Push 增强层输出路径（Promise + 超时 + OnPush<T>）")
	csRPCOutput := fs.String("cs-rpc", "", "C# RPC/Push 增强层输出路径（Task + 超时 + PushStream<T>）")
	fs.Parse(args)

	if *input == "" && *proto == "" {
		return fmt.Errorf("必须指定 -input 或 -proto")
	}
	if *input != "" && *proto != "" {
		return fmt.Errorf("-input 和 -proto 不能同时指定")
	}

	var msgs []codegen.MessageDef
	var err error
	if *proto != "" {
		msgs, err = codegen.ParseProtoFile(*proto)
	} else {
		msgs, err = codegen.ParseFile(*input)
	}
	if err != nil {
		return fmt.Errorf("解析失败: %w", err)
	}

	if len(msgs) == 0 {
		fmt.Println("未找到消息定义")
		return nil
	}
	fmt.Printf("发现 %d 个消息定义\n", len(msgs))

	type genTask struct {
		name    string
		path    *string
		genFunc func() ([]byte, error)
	}

	tasks := []genTask{
		{"Go 代码", output, func() ([]byte, error) { return codegen.GenerateGo(msgs, *pkg) }},
		{"TypeScript 类型", tsOutput, func() ([]byte, error) { return codegen.GenerateTS(msgs) }},
		{"TypeScript SDK", sdkOutput, func() ([]byte, error) { return codegen.GenerateTSSDK(msgs) }},
		{"C# 类型", csOutput, func() ([]byte, error) { return codegen.GenerateCSharp(msgs, *csNamespace) }},
		{"API 文档", docOutput, func() ([]byte, error) { return codegen.GenerateMarkdownDoc(msgs) }},
		{"TypeRegistry", registryOutput, func() ([]byte, error) { return codegen.GenerateTypeRegistry(msgs, *pkg) }},
		{"TypeScript RPC 增强", tsRPCOutput, func() ([]byte, error) { return codegen.GenerateTSRPCEnhance(msgs) }},
		{"C# RPC 增强", csRPCOutput, func() ([]byte, error) { return codegen.GenerateCSharpRPCEnhance(*csNamespace) }},
	}

	for _, task := range tasks {
		if *task.path == "" {
			continue
		}
		code, err := task.genFunc()
		if err != nil {
			return fmt.Errorf("生成%s失败: %w", task.name, err)
		}
		if err := os.WriteFile(*task.path, code, 0644); err != nil {
			return fmt.Errorf("写入文件失败: %w", err)
		}
		fmt.Printf("%s已生成: %s\n", task.name, *task.path)
	}

	return nil
}
