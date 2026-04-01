package actor

import "time"

// ReceiverMiddleware 接收中间件，包装Actor的消息处理
type ReceiverMiddleware func(Actor) Actor

// Props Actor配置蓝图
type Props struct {
	producer           Producer
	dispatcher         Dispatcher
	mailbox            func() Mailbox
	supervisorStrategy SupervisorStrategy
	receiveTimeout     time.Duration
	onInit             func(ctx Context)
	receiverMiddleware []ReceiverMiddleware
}

// PropsFromProducer 从生产者创建Props
func PropsFromProducer(producer Producer) *Props {
	return &Props{
		producer:           producer,
		dispatcher:         defaultDispatcher,
		mailbox:            NewDefaultMailbox,
		supervisorStrategy: RestartingStrategy,
	}
}

// PropsFromFunc 从函数创建Props
func PropsFromFunc(f ActorFunc) *Props {
	return PropsFromProducer(func() Actor {
		return f
	})
}

// WithDispatcher 设置调度器
func (props *Props) WithDispatcher(dispatcher Dispatcher) *Props {
	props.dispatcher = dispatcher
	return props
}

// WithMailbox 设置邮箱工厂
func (props *Props) WithMailbox(mailbox func() Mailbox) *Props {
	props.mailbox = mailbox
	return props
}

// WithSupervisor 设置监管策略
func (props *Props) WithSupervisor(strategy SupervisorStrategy) *Props {
	props.supervisorStrategy = strategy
	return props
}

// WithReceiveTimeout 设置接收超时
func (props *Props) WithReceiveTimeout(timeout time.Duration) *Props {
	props.receiveTimeout = timeout
	return props
}

// WithInit 设置初始化函数
func (props *Props) WithInit(onInit func(ctx Context)) *Props {
	props.onInit = onInit
	return props
}

// WithReceiverMiddleware 设置接收中间件链
func (props *Props) WithReceiverMiddleware(mw ...ReceiverMiddleware) *Props {
	props.receiverMiddleware = append(props.receiverMiddleware, mw...)
	return props
}

// spawn 创建Actor实例
func (props *Props) spawn(id string, parent *PID) (*PID, error) {
	actor := props.producer()

	// 应用中间件链（逆序包装，确保第一个中间件最外层）
	for i := len(props.receiverMiddleware) - 1; i >= 0; i-- {
		actor = props.receiverMiddleware[i](actor)
	}

	pid := NewLocalPID(id)

	cell := &actorCell{
		actor:              actor,
		producer:           props.producer,
		parent:             parent,
		self:               pid,
		children:           make(map[string]*PID),
		watchers:           make(map[string]*PID),
		watching:           make(map[string]*PID),
		supervisorStrategy: props.supervisorStrategy,
		receiveTimeout:     props.receiveTimeout,
		behavior:           NewBehaviorStack(actor.Receive),
	}

	cell.mailbox = props.mailbox()
	cell.mailbox.RegisterHandlers(cell.invokeUserMessage, cell.invokeSystemMessage)

	if mb, ok := cell.mailbox.(*defaultMailbox); ok {
		mb.SetScheduler(props.dispatcher)
	}

	pid.p = cell
	cell.mailbox.Start()

	return pid, nil
}
