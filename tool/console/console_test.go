package console

import (
	"strings"
	"testing"
)

// testCommand 测试用命令
type testCommand struct {
	name string
	help string
	out  string
}

func (c *testCommand) Name() string         { return c.name }
func (c *testCommand) Help() string         { return c.help }
func (c *testCommand) Run([]string) string { return c.out }

func TestRegisterAndHelp(t *testing.T) {
	// 保存原始命令列表，测试后恢复
	origCmds := commands
	defer func() { commands = origCmds }()

	// 重置为只有 help
	commands = nil
	Register(&helpCommand{})

	Register(&testCommand{name: "echo", help: "echo test", out: "echoed"})

	// 验证注册数量
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}

	// 验证 help 命令输出包含注册的命令
	helpCmd := commands[0]
	if helpCmd.Name() != "help" {
		t.Fatalf("first command should be help, got %s", helpCmd.Name())
	}

	output := helpCmd.Run(nil)
	if !strings.Contains(output, "echo") {
		t.Errorf("help output should contain 'echo', got: %s", output)
	}
	if !strings.Contains(output, "echo test") {
		t.Errorf("help output should contain help text, got: %s", output)
	}
}

func TestCommandLookup(t *testing.T) {
	origCmds := commands
	defer func() { commands = origCmds }()

	commands = nil
	Register(&helpCommand{})
	Register(&testCommand{name: "status", help: "show status", out: "running"})

	// 模拟命令查找逻辑
	tests := []struct {
		input string
		found bool
		out   string
	}{
		{"help", true, ""},
		{"status", true, "running"},
		{"unknown", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var cmd Command
			for _, c := range commands {
				if c.Name() == tt.input {
					cmd = c
					break
				}
			}

			if tt.found && cmd == nil {
				t.Error("expected to find command")
			}
			if !tt.found && cmd != nil {
				t.Error("expected not to find command")
			}

			if cmd != nil && tt.out != "" {
				result := cmd.Run(nil)
				if result != tt.out {
					t.Errorf("got %q, want %q", result, tt.out)
				}
			}
		})
	}
}

func TestNewConsole(t *testing.T) {
	c := NewConsole(9999)
	if c.Port != 9999 {
		t.Errorf("expected port 9999, got %d", c.Port)
	}
	if c.Prompt != "> " {
		t.Errorf("expected prompt '> ', got %q", c.Prompt)
	}
}

func TestConsoleZeroPort(t *testing.T) {
	c := NewConsole(0)
	// Start with port 0 should be a no-op
	c.Start()
	c.Close() // should not panic
}
