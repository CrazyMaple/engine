package gate

import (
	"encoding/binary"
	"time"

	"engine/log"
)

// AntiReplayFilter 防重放攻击过滤器
// 校验消息序列号严格递增 + 时间戳在合理窗口内
type AntiReplayFilter struct {
	timestampWindow time.Duration // 时间戳允许的偏差窗口
	seqOffset       int           // 序列号在消息中的字节偏移
	tsOffset        int           // 时间戳在消息中的字节偏移（Unix 秒，int64）
	minMsgLen       int           // 包含序列号和时间戳所需的最小消息长度
}

// NewAntiReplayFilter 创建防重放过滤器
// seqOffset: 序列号（uint64）在消息中的字节偏移
// tsOffset: 时间戳（int64, Unix秒）在消息中的字节偏移
// window: 时间戳允许的偏差窗口
func NewAntiReplayFilter(seqOffset, tsOffset int, window time.Duration) *AntiReplayFilter {
	minLen := seqOffset + 8
	if tsOffset+8 > minLen {
		minLen = tsOffset + 8
	}
	return &AntiReplayFilter{
		timestampWindow: window,
		seqOffset:       seqOffset,
		tsOffset:        tsOffset,
		minMsgLen:       minLen,
	}
}

func (f *AntiReplayFilter) Name() string { return "anti_replay" }

func (f *AntiReplayFilter) OnConnect(_ *SecurityContext) error {
	return nil
}

func (f *AntiReplayFilter) OnMessage(ctx *SecurityContext, data []byte) FilterResult {
	if len(data) < f.minMsgLen {
		// 消息太短，无法提取序列号/时间戳，跳过检查
		return FilterPass
	}

	// 提取序列号
	seqNum := binary.BigEndian.Uint64(data[f.seqOffset : f.seqOffset+8])

	// 校验序列号严格递增
	if ctx.LastSeqNum > 0 && seqNum <= ctx.LastSeqNum {
		log.Warn("[%s] conn=%s seq not increasing: got %d, last %d",
			f.Name(), ctx.ConnID, seqNum, ctx.LastSeqNum)
		ctx.AddViolation()
		return FilterReject
	}
	ctx.LastSeqNum = seqNum

	// 校验时间戳窗口
	if f.timestampWindow > 0 && f.tsOffset >= 0 {
		ts := int64(binary.BigEndian.Uint64(data[f.tsOffset : f.tsOffset+8]))
		msgTime := time.Unix(ts, 0)
		diff := time.Since(msgTime)
		if diff < 0 {
			diff = -diff
		}
		if diff > f.timestampWindow {
			log.Warn("[%s] conn=%s timestamp out of window: diff=%v, window=%v",
				f.Name(), ctx.ConnID, diff, f.timestampWindow)
			ctx.AddViolation()
			return FilterReject
		}
	}

	return FilterPass
}

func (f *AntiReplayFilter) OnDisconnect(_ *SecurityContext) {}
