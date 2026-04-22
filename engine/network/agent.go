package network

// Agent 代理接口
type Agent interface {
	Run()
	OnClose()
}
