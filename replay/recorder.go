package replay

import "time"

// Recorder 回放记录器，记录战斗过程中的所有输入事件
type Recorder struct {
	roomID    string
	startTime int64
	events    []ReplayEvent
	recording bool
}

// NewRecorder 创建回放记录器
func NewRecorder(roomID string) *Recorder {
	return &Recorder{
		roomID:    roomID,
		startTime: time.Now().UnixNano(),
		recording: true,
	}
}

// Record 记录一个事件
func (r *Recorder) Record(eventType uint16, data []byte) {
	if !r.recording {
		return
	}
	r.events = append(r.events, ReplayEvent{
		Timestamp: time.Now().UnixNano(),
		Type:      eventType,
		Data:      data,
	})
}

// RecordAt 记录一个指定时间戳的事件
func (r *Recorder) RecordAt(timestamp int64, eventType uint16, data []byte) {
	if !r.recording {
		return
	}
	r.events = append(r.events, ReplayEvent{
		Timestamp: timestamp,
		Type:      eventType,
		Data:      data,
	})
}

// Finish 结束录制，返回完整回放数据
func (r *Recorder) Finish() *ReplayData {
	r.recording = false

	var duration int64
	if len(r.events) > 0 {
		duration = r.events[len(r.events)-1].Timestamp - r.startTime
	}

	return &ReplayData{
		Version:   replayVersion,
		RoomID:    r.roomID,
		StartTime: r.startTime,
		Duration:  duration,
		Events:    r.events,
	}
}

// IsRecording 是否正在录制
func (r *Recorder) IsRecording() bool {
	return r.recording
}

// EventCount 已记录的事件数
func (r *Recorder) EventCount() int {
	return len(r.events)
}
