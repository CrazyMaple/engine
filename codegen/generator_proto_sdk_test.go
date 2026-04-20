package codegen

import (
	"strings"
	"testing"
)

func TestGenerateTSProtoSDK(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}
	code, err := GenerateTSProtoSDK(msgs)
	if err != nil {
		t.Fatal(err)
	}
	out := string(code)

	checks := []string{
		"export class TypeRegistry",
		"export class ProtobufAdapter",
		"export function registerAllMessages",
		"ProtoMessageIDs",
		"ProtoMessageNameToID",
		"ProtoMessageIDToName",
		`"LoginRequest": 1001`,
		"1001: \"LoginRequest\"",
		"export interface LoginRequest",
		"export interface ProtoMessageMap",
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("missing snippet %q", s)
		}
	}
}

func TestGenerateCSharpProtoSDK(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}
	code, err := GenerateCSharpProtoSDK(msgs, "MyGame.Proto")
	if err != nil {
		t.Fatal(err)
	}
	out := string(code)

	checks := []string{
		"namespace MyGame.Proto",
		"public static class ProtoMessageIDs",
		"public class ProtoTypeRegistry",
		"public class ProtobufCodecAdapter",
		"using Google.Protobuf;",
		"public const ushort LoginRequest = 1001;",
		"public static ProtoTypeRegistry CreateDefault()",
		"IdOf(string name)",
		"NameOf(ushort id)",
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("missing snippet %q", s)
		}
	}
}

func TestGenerateProtoRegistryGo(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}
	code, err := GenerateProtoRegistryGo(msgs, "shared")
	if err != nil {
		t.Fatal(err)
	}
	out := string(code)

	if !strings.Contains(out, "package shared") {
		t.Error("missing package declaration")
	}
	if !strings.Contains(out, "ProtoMsgID_LoginRequest uint16 = 1001") {
		t.Error("missing LoginRequest ID const")
	}
	if !strings.Contains(out, "var ProtoMessageName") {
		t.Error("missing ProtoMessageName map")
	}
	if !strings.Contains(out, "var ProtoMessageID") {
		t.Error("missing ProtoMessageID map")
	}
}

func TestGenerateTSProtoExample(t *testing.T) {
	code := GenerateTSProtoExample()
	out := string(code)
	for _, s := range []string{
		"import * as protobuf",
		"registerAllMessages",
		"ProtobufAdapter",
		"new GameClient",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing snippet %q", s)
		}
	}
}

func TestDetectRPCPairs(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}
	pairs := DetectRPCPairs(msgs)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 RPC pair, got %d: %+v", len(pairs), pairs)
	}
	if pairs[0].Request != "LoginRequest" || pairs[0].Response != "LoginResponse" {
		t.Errorf("unexpected pair: %+v", pairs[0])
	}
}

func TestGenerateTSRPCEnhance(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}
	code, err := GenerateTSRPCEnhance(msgs)
	if err != nil {
		t.Fatal(err)
	}
	out := string(code)

	checks := []string{
		"export interface RPCMap",
		`LoginRequest: "LoginResponse";`,
		"export class RPCClient",
		"export class PushSubscriber",
		"export class RPCTimeoutError",
		"export class RPCAbortError",
		"RPCCallOptions",
		"timeoutMs",
		"AbortSignal",
		"__rpc_id",
		"onPush<K extends keyof MessageMap>",
		"once<K extends keyof MessageMap>",
		"[Symbol.asyncIterator]",
		"export function createRPC",
		"export function createPush",
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("missing snippet %q", s)
		}
	}
}

func TestGenerateTSRPCEnhanceNoPairs(t *testing.T) {
	// 仅有一个消息（无 Request/Response 对）时，RPCMap 应为空但不应报错
	msgs := []MessageDef{{Name: "ChatMessage", ID: 1, Fields: nil}}
	code, err := GenerateTSRPCEnhance(msgs)
	if err != nil {
		t.Fatal(err)
	}
	out := string(code)
	if !strings.Contains(out, "export interface RPCMap") {
		t.Error("missing RPCMap interface")
	}
	// 无配对时，RPCPairs 对象不应包含任何键
	if strings.Contains(out, "ChatMessage") {
		t.Error("ChatMessage should not appear in RPCPairs when no response exists")
	}
}

func TestGenerateUnityProtoExample(t *testing.T) {
	code, err := GenerateUnityProtoExample("MyGame.Proto")
	if err != nil {
		t.Fatal(err)
	}
	out := string(code)
	if !strings.Contains(out, "namespace MyGame.Proto.Example") {
		t.Error("missing nested namespace")
	}
	if !strings.Contains(out, "ProtoTypeRegistry.CreateDefault()") {
		t.Error("missing CreateDefault call")
	}
}

func TestProtoSDKConsistentIDs(t *testing.T) {
	// 验证 TS 和 C# 输出使用相同的消息 ID
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := GenerateTSProtoSDK(msgs)
	cs, _ := GenerateCSharpProtoSDK(msgs, "X")
	goReg, _ := GenerateProtoRegistryGo(msgs, "x")

	for _, m := range msgs {
		tsIDFrag := "\"" + m.Name + "\": "
		if !strings.Contains(string(ts), tsIDFrag) {
			t.Errorf("TS missing id entry for %s", m.Name)
		}
		csIDFrag := "public const ushort " + m.Name + " ="
		if !strings.Contains(string(cs), csIDFrag) {
			t.Errorf("C# missing id const for %s", m.Name)
		}
		goIDFrag := "ProtoMsgID_" + m.Name + " uint16 ="
		if !strings.Contains(string(goReg), goIDFrag) {
			t.Errorf("Go registry missing id const for %s", m.Name)
		}
	}
}
