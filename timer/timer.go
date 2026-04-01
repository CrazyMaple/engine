package timer

import "time"

// Dispatcher 定时器调度器
type Dispatcher struct {
	ChanTimer chan *Timer
}

// NewDispatcher 创建定时器调度器
func NewDispatcher(chanSize int) *Dispatcher {
	return &Dispatcher{
		ChanTimer: make(chan *Timer, chanSize),
	}
}

// Timer 定时器
type Timer struct {
	t  *time.Timer
	cb func()
}

// Stop 停止定时器
func (t *Timer) Stop() {
	if t.t != nil {
		t.t.Stop()
	}
	t.cb = nil
}

// Cb 执行回调
func (t *Timer) Cb() {
	defer func() {
		t.cb = nil
		if r := recover(); r != nil {
			// 记录panic但不中断
		}
	}()

	if t.cb != nil {
		t.cb()
	}
}

// AfterFunc 延迟执行
func (disp *Dispatcher) AfterFunc(d time.Duration, cb func()) *Timer {
	t := &Timer{cb: cb}
	t.t = time.AfterFunc(d, func() {
		disp.ChanTimer <- t
	})
	return t
}

// Cron 定时任务
type Cron struct {
	t *Timer
}

// Stop 停止定时任务
func (c *Cron) Stop() {
	if c.t != nil {
		c.t.Stop()
	}
}

// CronFunc 创建Cron定时任务
func (disp *Dispatcher) CronFunc(cronExpr *CronExpr, _cb func()) *Cron {
	c := &Cron{}

	now := time.Now()
	nextTime := cronExpr.Next(now)
	if nextTime.IsZero() {
		return c
	}

	var cb func()
	cb = func() {
		defer _cb()

		now := time.Now()
		nextTime := cronExpr.Next(now)
		if nextTime.IsZero() {
			return
		}
		c.t = disp.AfterFunc(nextTime.Sub(now), cb)
	}

	c.t = disp.AfterFunc(nextTime.Sub(now), cb)
	return c
}
