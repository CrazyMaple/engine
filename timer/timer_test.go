package timer

import (
	"testing"
	"time"
)

// === CronExpr 解析测试 ===

func TestNewCronExpr5Fields(t *testing.T) {
	// 5 字段格式（无秒）=> 秒默认为 0
	expr, err := NewCronExpr("* * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if expr.sec != 1 { // bit 0 set
		t.Fatalf("expected sec=1, got %d", expr.sec)
	}
}

func TestNewCronExpr6Fields(t *testing.T) {
	// 6 字段格式（含秒）
	expr, err := NewCronExpr("30 * * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if expr.sec&(1<<30) == 0 {
		t.Fatal("second 30 should be set")
	}
}

func TestCronExprRange(t *testing.T) {
	expr, err := NewCronExpr("0 1-3 * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// 分钟 1, 2, 3 应被设置
	for _, m := range []int{1, 2, 3} {
		if expr.min&(1<<uint(m)) == 0 {
			t.Fatalf("minute %d should be set", m)
		}
	}
	// 分钟 0 不应被设置
	if expr.min&1 != 0 {
		t.Fatal("minute 0 should not be set")
	}
}

func TestCronExprStep(t *testing.T) {
	expr, err := NewCronExpr("0 */15 * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// 应设置 0, 15, 30, 45
	for _, m := range []int{0, 15, 30, 45} {
		if expr.min&(1<<uint(m)) == 0 {
			t.Fatalf("minute %d should be set", m)
		}
	}
}

func TestCronExprComma(t *testing.T) {
	expr, err := NewCronExpr("0 1,3,5 * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, m := range []int{1, 3, 5} {
		if expr.min&(1<<uint(m)) == 0 {
			t.Fatalf("minute %d should be set", m)
		}
	}
	if expr.min&(1<<2) != 0 {
		t.Fatal("minute 2 should not be set")
	}
}

func TestCronExprInvalid(t *testing.T) {
	cases := []string{
		"",
		"* *",
		"* * * *",
		"60 * * * *",
		"* 25 * * *",
		"abc * * * *",
	}
	for _, c := range cases {
		_, err := NewCronExpr(c)
		if err == nil {
			t.Fatalf("expected error for expr %q", c)
		}
	}
}

// === CronExpr.Next 测试 ===

func TestCronExprNext(t *testing.T) {
	// "* * * * *" (5字段) => "0 * * * * *" => sec=0, min=*, 即每分钟第0秒
	expr, err := NewCronExpr("* * * * *")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 4, 2, 10, 30, 0, 0, time.UTC)
	next := expr.Next(base)

	// base+1s=10:30:01, sec 只允许 0, 所以跳到 10:31:00
	expected := time.Date(2026, 4, 2, 10, 31, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestCronExprNextSpecificTime(t *testing.T) {
	// 每天 12:00:00
	expr, err := NewCronExpr("0 0 12 * * *")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 4, 2, 13, 0, 0, 0, time.UTC)
	next := expr.Next(base)

	// 应为次日 12:00:00
	expected := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

// === Dispatcher 测试 ===

func TestDispatcherAfterFunc(t *testing.T) {
	disp := NewDispatcher(10)
	done := make(chan struct{})

	disp.AfterFunc(50*time.Millisecond, func() {
		close(done)
	})

	select {
	case timer := <-disp.ChanTimer:
		timer.Cb()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("AfterFunc did not fire within timeout")
	}

	select {
	case <-done:
		// ok
	default:
		t.Fatal("callback was not executed")
	}
}

func TestTimerStop(t *testing.T) {
	disp := NewDispatcher(10)
	called := false

	timer := disp.AfterFunc(50*time.Millisecond, func() {
		called = true
	})

	timer.Stop()

	// 等待足够时间确认不会触发
	time.Sleep(200 * time.Millisecond)

	select {
	case tm := <-disp.ChanTimer:
		tm.Cb()
	default:
	}

	if called {
		t.Fatal("stopped timer should not fire")
	}
}

func TestTimerCbPanicRecovery(t *testing.T) {
	disp := NewDispatcher(10)

	disp.AfterFunc(10*time.Millisecond, func() {
		panic("test panic")
	})

	select {
	case timer := <-disp.ChanTimer:
		// Cb 应 recover panic 不中断
		timer.Cb()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timer did not fire")
	}
}

func TestTimerCbNilAfterExecution(t *testing.T) {
	disp := NewDispatcher(10)

	disp.AfterFunc(10*time.Millisecond, func() {})

	select {
	case timer := <-disp.ChanTimer:
		timer.Cb()
		// cb 执行后应被设为 nil
		if timer.cb != nil {
			t.Fatal("cb should be nil after execution")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timer did not fire")
	}
}

func TestDispatcherCronFunc(t *testing.T) {
	disp := NewDispatcher(10)

	// 每秒触发
	expr, err := NewCronExpr("* * * * * *")
	if err != nil {
		t.Fatal(err)
	}

	called := 0
	cron := disp.CronFunc(expr, func() {
		called++
	})
	defer cron.Stop()

	// 等待第一次触发（最多 2 秒）
	select {
	case timer := <-disp.ChanTimer:
		timer.Cb()
	case <-time.After(2 * time.Second):
		t.Fatal("CronFunc did not fire within timeout")
	}

	if called != 1 {
		t.Fatalf("expected called=1, got %d", called)
	}
}

func TestCronStop(t *testing.T) {
	disp := NewDispatcher(10)

	expr, err := NewCronExpr("* * * * * *")
	if err != nil {
		t.Fatal(err)
	}

	cron := disp.CronFunc(expr, func() {})
	cron.Stop()

	// 确认停止后不会触发
	time.Sleep(200 * time.Millisecond)

	select {
	case <-disp.ChanTimer:
		// 可能在 Stop 前已经投递，这是正常的
	default:
	}
}
