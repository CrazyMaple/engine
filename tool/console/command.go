package console

// Command 控制台命令接口
type Command interface {
	Name() string
	Help() string
	Run(args []string) string
}

var commands []Command

// Register 注册命令
func Register(cmd Command) {
	commands = append(commands, cmd)
}

// helpCommand 帮助命令
type helpCommand struct{}

func (c *helpCommand) Name() string {
	return "help"
}

func (c *helpCommand) Help() string {
	return "show available commands"
}

func (c *helpCommand) Run(args []string) string {
	output := "available commands:\n"
	for _, cmd := range commands {
		output += "  " + cmd.Name() + " - " + cmd.Help() + "\n"
	}
	return output
}

func init() {
	Register(&helpCommand{})
}
