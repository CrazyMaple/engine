package bench

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"gamelib/syncx"
)

// 状态同步 Delta vs Full 快照带宽对比基准。
//
// 对比三条路径:
//   - full-snapshot     :  每帧都通过 DeltaEncoder.Snapshot() 发送全量状态
//   - delta-steady      :  稳定状态,每帧仅少量字段变化(~10% 实体,每实体 1 字段)
//   - delta-burst       :  突发状态,每帧较多字段变化(~50% 实体,每实体 3 字段)
//
// 每个子基准测量:
//   - ns/op           :  编码耗时
//   - p50/p95/p99     :  延迟分位
//   - bytes/frame     :  单帧二进制大小(带宽代理指标)
//
// 运行方式:
//
//	go test ./bench/ -bench=BenchmarkStateSyncDelta -benchmem -benchtime=2s
//
// 典型观察:entity=100 field=8 时,delta-steady 单帧可能仅 ~50B,
// 全量快照 ~2KB,带宽节省约 40 倍。

const (
	benchEntityCount = 100
	benchFieldCount  = 8
)

// fieldNames 稳定字段名列表(受 DeltaSchema 单实体 64 字段上限约束)
var fieldNames = []string{"hp", "mp", "x", "y", "z", "vx", "vy", "vz", "dir", "state"}

func makeInitialState(seed int64) map[string]map[string]interface{} {
	r := rand.New(rand.NewSource(seed))
	state := make(map[string]map[string]interface{}, benchEntityCount)
	for i := 0; i < benchEntityCount; i++ {
		eid := fmt.Sprintf("e%04d", i)
		fields := make(map[string]interface{}, benchFieldCount)
		for j := 0; j < benchFieldCount; j++ {
			name := fieldNames[j]
			switch j % 3 {
			case 0:
				fields[name] = int64(r.Intn(1000))
			case 1:
				fields[name] = r.Float64() * 1000
			case 2:
				fields[name] = r.Intn(2) == 0
			}
		}
		state[eid] = fields
	}
	return state
}

// mutateState 按比例修改字段:mutateRatio 为被修改的实体比例,
// fieldsPerEntity 为每个被选中实体修改的字段数。
func mutateState(state map[string]map[string]interface{}, r *rand.Rand, mutateRatio float64, fieldsPerEntity int) {
	targetCount := int(float64(len(state)) * mutateRatio)
	if targetCount == 0 {
		targetCount = 1
	}
	i := 0
	for eid, fields := range state {
		if i >= targetCount {
			break
		}
		for k := 0; k < fieldsPerEntity; k++ {
			name := fieldNames[r.Intn(benchFieldCount)]
			switch r.Intn(3) {
			case 0:
				fields[name] = int64(r.Intn(1000))
			case 1:
				fields[name] = r.Float64() * 1000
			case 2:
				fields[name] = r.Intn(2) == 0
			}
		}
		_ = eid
		i++
	}
}

// BenchmarkStateSyncDelta 对比 Delta 与 Full 快照的编码耗时及带宽消耗。
func BenchmarkStateSyncDelta(b *testing.B) {
	cases := []struct {
		name            string
		mutateRatio     float64
		fieldsPerEntity int
		fullSnapshot    bool
	}{
		{"full-snapshot", 0, 0, true},
		{"delta-steady-10pct-1field", 0.10, 1, false},
		{"delta-burst-50pct-3field", 0.50, 3, false},
	}

	for _, c := range cases {
		c := c
		b.Run(c.name, func(b *testing.B) {
			schema := syncx.NewDeltaSchema()
			for _, fn := range fieldNames {
				schema.Register(fn)
			}
			enc := syncx.NewDeltaEncoder(schema)

			state := makeInitialState(42)
			enc.Encode(0, state)

			r := rand.New(rand.NewSource(7))
			rec := NewLatencyRecorder(b.N)
			var totalBytes int64

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if !c.fullSnapshot {
					mutateState(state, r, c.mutateRatio, c.fieldsPerEntity)
				}

				t0 := time.Now()
				var fd *syncx.FrameDelta
				if c.fullSnapshot {
					fd = enc.Snapshot(uint64(i + 1))
				} else {
					fd = enc.Encode(uint64(i+1), state)
				}
				buf, err := syncx.MarshalDelta(fd, schema)
				if err != nil {
					b.Fatal(err)
				}
				rec.Add(time.Since(t0).Nanoseconds())
				totalBytes += int64(len(buf))
			}

			b.StopTimer()
			reportPercentiles(b, rec)
			avgBytes := float64(totalBytes) / float64(b.N)
			b.ReportMetric(avgBytes, "bytes/frame")
			b.SetBytes(int64(avgBytes))
		})
	}
}

// BenchmarkStateSyncDeltaRoundtrip 端到端 Encode -> Marshal -> Unmarshal -> Apply 对比。
func BenchmarkStateSyncDeltaRoundtrip(b *testing.B) {
	cases := []struct {
		name         string
		fullSnapshot bool
	}{
		{"full-snapshot", true},
		{"delta-steady-10pct-1field", false},
	}

	for _, c := range cases {
		c := c
		b.Run(c.name, func(b *testing.B) {
			schema := syncx.NewDeltaSchema()
			for _, fn := range fieldNames {
				schema.Register(fn)
			}
			enc := syncx.NewDeltaEncoder(schema)
			dec := syncx.NewDeltaDecoder(schema)

			state := makeInitialState(42)
			// 先 warmup 填充编码器快照
			fd := enc.Encode(0, state)
			buf, err := syncx.MarshalDelta(fd, schema)
			if err != nil {
				b.Fatal(err)
			}
			fd2, err := syncx.UnmarshalDelta(buf, schema)
			if err != nil {
				b.Fatal(err)
			}
			dec.Apply(fd2)

			r := rand.New(rand.NewSource(11))
			rec := NewLatencyRecorder(b.N)
			var totalBytes int64

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if !c.fullSnapshot {
					mutateState(state, r, 0.10, 1)
				}
				t0 := time.Now()
				var fd *syncx.FrameDelta
				if c.fullSnapshot {
					fd = enc.Snapshot(uint64(i + 1))
				} else {
					fd = enc.Encode(uint64(i+1), state)
				}
				buf, err := syncx.MarshalDelta(fd, schema)
				if err != nil {
					b.Fatal(err)
				}
				fd2, err := syncx.UnmarshalDelta(buf, schema)
				if err != nil {
					b.Fatal(err)
				}
				dec.Apply(fd2)
				rec.Add(time.Since(t0).Nanoseconds())
				totalBytes += int64(len(buf))
			}

			b.StopTimer()
			reportPercentiles(b, rec)
			avgBytes := float64(totalBytes) / float64(b.N)
			b.ReportMetric(avgBytes, "bytes/frame")
			b.SetBytes(int64(avgBytes))
		})
	}
}
