package console

import (
	"bufio"
	"engine/network"
	"math"
	"strconv"
	"strings"
)

var server *network.TCPServer

// Console 运维控制台
type Console struct {
	Port   int
	Prompt string
}

// NewConsole 创建控制台
func NewConsole(port int) *Console {
	return &Console{
		Port:   port,
		Prompt: "> ",
	}
}

// Start 启动控制台
func (c *Console) Start() {
	if c.Port == 0 {
		return
	}

	server = &network.TCPServer{
		Addr:            "localhost:" + strconv.Itoa(c.Port),
		MaxConnNum:      math.MaxInt32,
		PendingWriteNum: 100,
		NewAgent:        c.newAgent,
	}

	server.Start()
}

// Close 关闭控制台
func (c *Console) Close() {
	if server != nil {
		server.Close()
	}
}

func (c *Console) newAgent(conn *network.TCPConn) network.Agent {
	return &consoleAgent{
		conn:    conn,
		reader:  bufio.NewReader(conn),
		console: c,
	}
}

// consoleAgent 控制台代理
type consoleAgent struct {
	conn    *network.TCPConn
	reader  *bufio.Reader
	console *Console
}

func (a *consoleAgent) Run() {
	for {
		if a.console.Prompt != "" {
			a.conn.Write([]byte(a.console.Prompt))
		}

		line, err := a.reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSuffix(line[:len(line)-1], "\r")

		args := strings.Fields(line)
		if len(args) == 0 {
			continue
		}
		if args[0] == "quit" {
			break
		}

		var cmd Command
		for _, c := range commands {
			if c.Name() == args[0] {
				cmd = c
				break
			}
		}
		if cmd == nil {
			a.conn.Write([]byte("command not found, try `help` for help\r\n"))
			continue
		}

		output := cmd.Run(args[1:])
		if output != "" {
			a.conn.Write([]byte(output + "\r\n"))
		}
	}
}

func (a *consoleAgent) OnClose() {}

