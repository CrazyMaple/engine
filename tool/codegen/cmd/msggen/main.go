package main

import (
	"flag"
	"fmt"
	"os"

	"tool/codegen"
)

func main() {
	input := flag.String("input", "", "输入 Go 源文件（包含消息结构体定义）")
	proto := flag.String("proto", "", "输入 .proto 文件（与 -input 互斥）")
	output := flag.String("output", "", "输出 Go 文件路径")
	pkg := flag.String("pkg", "main", "生成代码的包名")
	tsOutput := flag.String("ts", "", "可选：TypeScript 类型定义输出路径")
	sdkOutput := flag.String("sdk", "", "可选：TypeScript 完整 SDK 输出路径（含 WebSocket 连接管理）")
	csOutput := flag.String("cs", "", "可选：C# 类型定义输出路径（Unity 客户端）")
	csSDKOutput := flag.String("cs-sdk", "", "可选：C# 完整 SDK 输出路径（含连接管理、消息路由）")
	csNamespace := flag.String("cs-ns", "GameMessages", "C# 命名空间")
	docOutput := flag.String("doc", "", "可选：Markdown API 文档输出路径")
	registryOutput := flag.String("registry", "", "可选：TypeRegistry 注册代码输出路径")
	sdkV2Output := flag.String("sdk-v2", "", "可选：增强版 TypeScript SDK（消息路由器 + Protobuf 支持）")
	unityPkg := flag.String("unity-pkg", "", "可选：Unity Package 输出目录（生成可直接导入的 Unity Package 结构）")
	tsProtoSDK := flag.String("ts-proto-sdk", "", "可选：TypeScript Protobuf SDK 输出路径（基于 protobuf.js 的 TypeRegistry + Adapter）")
	csProtoSDK := flag.String("cs-proto-sdk", "", "可选：C# Protobuf SDK 输出路径（基于 Google.Protobuf 的 TypeRegistry + Adapter）")
	protoRegistryGo := flag.String("proto-registry", "", "可选：Proto 消息 ID 常量表 Go 源文件输出路径（服务端与客户端共享 ID）")
	protoExampleDir := flag.String("proto-example", "", "可选：生成 Protobuf 示例项目目录（TypeScript + Unity 骨架）")
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

	// 生成 C# 完整 SDK（含连接管理、消息路由）
	if *csSDKOutput != "" {
		code, err := codegen.GenerateCSharpSDK(msgs, *csNamespace)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 C# SDK 失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*csSDKOutput, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("C# SDK 已生成: %s\n", *csSDKOutput)
	}

	// 生成增强版 TypeScript SDK（消息路由器 + Protobuf 支持）
	if *sdkV2Output != "" {
		code, err := codegen.GenerateTSSDKEnhanced(msgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成增强版 TS SDK 失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*sdkV2Output, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("增强版 TypeScript SDK 已生成: %s\n", *sdkV2Output)
	}

	// 生成 Unity Package 目录结构
	if *unityPkg != "" {
		if err := generateUnityPackage(msgs, *unityPkg, *csNamespace); err != nil {
			fmt.Fprintf(os.Stderr, "生成 Unity Package 失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Unity Package 已生成: %s\n", *unityPkg)
	}

	// 生成 TypeScript Protobuf SDK
	if *tsProtoSDK != "" {
		code, err := codegen.GenerateTSProtoSDK(msgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 TypeScript Protobuf SDK 失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*tsProtoSDK, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("TypeScript Protobuf SDK 已生成: %s\n", *tsProtoSDK)
	}

	// 生成 C# Protobuf SDK
	if *csProtoSDK != "" {
		code, err := codegen.GenerateCSharpProtoSDK(msgs, *csNamespace)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 C# Protobuf SDK 失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*csProtoSDK, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("C# Protobuf SDK 已生成: %s\n", *csProtoSDK)
	}

	// 生成 Proto 消息 ID 常量表（服务端与客户端共享）
	if *protoRegistryGo != "" {
		code, err := codegen.GenerateProtoRegistryGo(msgs, *pkg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成 Proto 常量表失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*protoRegistryGo, code, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Proto 常量表已生成: %s\n", *protoRegistryGo)
	}

	// 生成 Protobuf 示例项目骨架
	if *protoExampleDir != "" {
		if err := generateProtoExamples(msgs, *protoExampleDir, *csNamespace); err != nil {
			fmt.Fprintf(os.Stderr, "生成 Protobuf 示例项目失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Protobuf 示例项目已生成: %s\n", *protoExampleDir)
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

// generateUnityPackage 生成可直接导入 Unity 工程的 Package 目录结构
func generateUnityPackage(msgs []codegen.MessageDef, dir, namespace string) error {
	// Unity Package 标准目录结构
	dirs := []string{
		dir,
		dir + "/Runtime",
		dir + "/Runtime/Scripts",
		dir + "/Editor",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// 1. package.json — Unity Package Manager 清单
	packageJSON := fmt.Sprintf(`{
  "name": "com.engine.sdk",
  "version": "1.0.0",
  "displayName": "Engine Game Client SDK",
  "description": "Auto-generated game client SDK with TCP/WebSocket connection management",
  "unity": "2020.3",
  "dependencies": {
    "com.unity.nuget.newtonsoft-json": "3.0.0"
  },
  "keywords": ["game", "networking", "sdk"]
}
`)
	if err := os.WriteFile(dir+"/package.json", []byte(packageJSON), 0644); err != nil {
		return err
	}

	// 2. Runtime Assembly Definition
	asmdef := fmt.Sprintf(`{
  "name": "%s",
  "rootNamespace": "%s",
  "references": [],
  "includePlatforms": [],
  "excludePlatforms": [],
  "allowUnsafeCode": false
}
`, namespace, namespace)
	if err := os.WriteFile(dir+"/Runtime/"+namespace+".asmdef", []byte(asmdef), 0644); err != nil {
		return err
	}

	// 3. C# SDK 文件
	sdkCode, err := codegen.GenerateCSharpSDK(msgs, namespace)
	if err != nil {
		return fmt.Errorf("generate C# SDK: %w", err)
	}
	if err := os.WriteFile(dir+"/Runtime/Scripts/GameClient.cs", sdkCode, 0644); err != nil {
		return err
	}

	// 4. Editor Assembly Definition
	editorAsmdef := fmt.Sprintf(`{
  "name": "%s.Editor",
  "rootNamespace": "%s.Editor",
  "references": ["%s"],
  "includePlatforms": ["Editor"],
  "excludePlatforms": []
}
`, namespace, namespace, namespace)
	if err := os.WriteFile(dir+"/Editor/"+namespace+".Editor.asmdef", []byte(editorAsmdef), 0644); err != nil {
		return err
	}

	return nil
}

// generateProtoExamples 生成 Protobuf 示例项目骨架（TypeScript + Unity）
// 目录结构:
//   <dir>/typescript/
//     ├── proto_sdk.ts           # Protobuf SDK
//     ├── main.ts                # 示例入口
//     └── package.json           # npm 清单
//   <dir>/unity/
//     ├── ProtoTypeRegistry.cs   # C# Protobuf SDK
//     └── ProtobufClientExample.cs
func generateProtoExamples(msgs []codegen.MessageDef, dir, namespace string) error {
	tsDir := dir + "/typescript"
	unityDir := dir + "/unity"
	for _, d := range []string{dir, tsDir, unityDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// TypeScript SDK
	tsSDK, err := codegen.GenerateTSProtoSDK(msgs)
	if err != nil {
		return fmt.Errorf("generate ts proto sdk: %w", err)
	}
	if err := os.WriteFile(tsDir+"/proto_sdk.ts", tsSDK, 0644); err != nil {
		return err
	}
	// TypeScript 示例入口
	if err := os.WriteFile(tsDir+"/main.ts", codegen.GenerateTSProtoExample(), 0644); err != nil {
		return err
	}
	// package.json
	tsPkg := `{
  "name": "engine-proto-client-example",
  "version": "1.0.0",
  "description": "Engine Protobuf client SDK example",
  "type": "module",
  "dependencies": {
    "protobufjs": "^7.2.5"
  },
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}
`
	if err := os.WriteFile(tsDir+"/package.json", []byte(tsPkg), 0644); err != nil {
		return err
	}

	// Unity C# Protobuf SDK
	csSDK, err := codegen.GenerateCSharpProtoSDK(msgs, namespace)
	if err != nil {
		return fmt.Errorf("generate cs proto sdk: %w", err)
	}
	if err := os.WriteFile(unityDir+"/ProtoTypeRegistry.cs", csSDK, 0644); err != nil {
		return err
	}
	// Unity 示例 MonoBehaviour
	unityExample, err := codegen.GenerateUnityProtoExample(namespace)
	if err != nil {
		return fmt.Errorf("generate unity example: %w", err)
	}
	if err := os.WriteFile(unityDir+"/ProtobufClientExample.cs", unityExample, 0644); err != nil {
		return err
	}

	return nil
}
