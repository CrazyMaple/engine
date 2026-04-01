package actor

// DeadLetterEvent 死信事件
type DeadLetterEvent struct {
	PID     *PID
	Message interface{}
	Sender  *PID
}

// deadLetterProcess 死信进程
type deadLetterProcess struct {
	eventStream *EventStream
}

func newDeadLetterProcess(eventStream *EventStream) *deadLetterProcess {
	return &deadLetterProcess{
		eventStream: eventStream,
	}
}

func (d *deadLetterProcess) SendUserMessage(pid *PID, message interface{}) {
	d.eventStream.Publish(&DeadLetterEvent{
		PID:     pid,
		Message: message,
	})
}

func (d *deadLetterProcess) SendSystemMessage(pid *PID, message interface{}) {
	d.eventStream.Publish(&DeadLetterEvent{
		PID:     pid,
		Message: message,
	})
}

func (d *deadLetterProcess) Stop(pid *PID) {
	// 死信进程不能被停止
}
