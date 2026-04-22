package actor

// Actor 是所有Actor必须实现的接口
type Actor interface {
	Receive(ctx Context)
}

// ActorFunc 将函数转换为Actor
type ActorFunc func(ctx Context)

func (f ActorFunc) Receive(ctx Context) {
	f(ctx)
}

// Producer 是创建Actor实例的工厂函数
type Producer func() Actor
