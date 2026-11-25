package replay_test

import (
	"bytes"
	"fmt"
	"testing"

	"goframe/api"
	"goframe/internal/drift"
	"goframe/replay"
)

// worstCaseFrames 构造单实体、每帧一条“半格”冲量的最坏情况序列，
// 使每帧量化误差同向且绝对值恰为 floor(Q/2)。
func worstCaseFrames(n int, q int64) []replay.Frame {
	emax := q / 2
	frames := make([]replay.Frame, n)
	for f := 0; f < n; f++ {
		frames[f] = replay.Frame{Commands: []api.Command{{
			Tick: f, Body: 0, PushX: int(emax),
		}}}
	}
	return frames
}

// TestWorstCaseAmplificationExact 证明误差经积分累积后被放大，且公式界是紧的：
// 最坏情况序列的末帧位置漂移精确等于 Emax*dt*N(N+1)/2。
func TestWorstCaseAmplificationExact(t *testing.T) {
	cases := []struct {
		q  int64
		n  int
		dt int
	}{
		{256, 1, 1}, {256, 20, 4}, {256, 40, 4},
		{1024, 20, 1}, {1024, 20, 8}, {64, 30, 2},
		{1249, 20, 4}, // 反解产生的奇数网格
	}
	for _, c := range cases {
		name := fmt.Sprintf("q=%d/n=%d/dt=%d", c.q, c.n, c.dt)
		t.Run(name, func(t *testing.T) {
			opt := api.Options{Bodies: 1, SubSteps: c.dt}
			frames := worstCaseFrames(c.n, c.q)
			qlog := replay.QuantizeLogToStep(replay.Log{Options: opt, Frames: frames}, int(c.q))

			ref, _, err := replay.RunSnapshots(opt, frames)
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := replay.RunSnapshots(opt, qlog.Frames)
			if err != nil {
				t.Fatal(err)
			}
			_, _, finalDrift := replay.MaxComponentDrift(ref, got)
			want := drift.BoundRaw(c.q, c.n, c.dt)
			if finalDrift != want {
				t.Fatalf("末帧漂移=%d, 期望精确等于公式界 %d", finalDrift, want)
			}
			// 放大倍数就是权重和 W；N>1 时显著大于单步误差 Emax。
			if c.n > 1 && drift.WeightSum(c.n, c.dt) <= 1 {
				t.Fatal("权重和应大于 1")
			}
		})
	}
}

// TestBoundHoldsEveryTick 用确定性多实体指令流验证：任意一帧的实测漂移
// 都不超过按当前帧数 n 计算的理论界。
func TestBoundHoldsEveryTick(t *testing.T) {
	const n, b, dt = 24, 64, 3
	opt := api.Options{Bodies: b, SubSteps: dt, Gravity: 2}
	frames := deterministicFrames(n, b)
	ref, _, err := replay.RunSnapshots(opt, frames)
	if err != nil {
		t.Fatal(err)
	}
	for _, bits := range []int{6, 8, 10} {
		qlog := replay.QuantizeLog(replay.Log{Options: opt, Frames: frames}, bits)
		qs, _, err := replay.RunSnapshots(opt, qlog.Frames)
		if err != nil {
			t.Fatal(err)
		}
		q := int64(1) << (16 - bits)
		for k := 1; k <= n; k++ {
			d, _, _ := replay.MaxComponentDrift(ref[:k], qs[:k])
			bound := drift.BoundRaw(q, k, dt)
			if d > bound {
				t.Fatalf("bits=%d tick=%d 漂移 %d 越界 %d", bits, k-1, d, bound)
			}
		}
	}
}

// TestThreeTiersUnderCap 复现任务参数：N=20, dt=4, Δ=8 单位，
// 6/8/10 三档实测漂移都必须低于上限。
func TestThreeTiersUnderCap(t *testing.T) {
	const n, b, dt = 20, 128, 4
	capRaw := int64(8) << 16
	opt := api.Options{Bodies: b, SubSteps: dt, Gravity: 2}
	frames := deterministicFrames(n, b)
	ref, _, err := replay.RunSnapshots(opt, frames)
	if err != nil {
		t.Fatal(err)
	}
	for _, bits := range []int{6, 8, 10} {
		qlog := replay.QuantizeLog(replay.Log{Options: opt, Frames: frames}, bits)
		qs, _, err := replay.RunSnapshots(opt, qlog.Frames)
		if err != nil {
			t.Fatal(err)
		}
		d, _, _ := replay.MaxComponentDrift(ref, qs)
		if d > capRaw {
			t.Fatalf("%d 位档漂移 %d 超过上限 %d", bits, d, capRaw)
		}
	}
}

// TestInversionBackSubstitution 反解 Q_max 并回代：
// Q_max 最坏情况漂移 ≤ cap，而 Q_max+1 的最坏情况漂移 > cap。
func TestInversionBackSubstitution(t *testing.T) {
	const capRaw = int64(8) << 16
	const n, dt = 20, 4
	qmax := drift.MaxIntegerStepRaw(capRaw, n, dt)
	if qmax != 1249 {
		t.Fatalf("Qmax=%d want 1249", qmax)
	}
	opt := api.Options{Bodies: 1, SubSteps: dt}

	run := func(q int64) int64 {
		frames := worstCaseFrames(n, q)
		qlog := replay.QuantizeLogToStep(replay.Log{Options: opt, Frames: frames}, int(q))
		ref, _, err := replay.RunSnapshots(opt, frames)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := replay.RunSnapshots(opt, qlog.Frames)
		if err != nil {
			t.Fatal(err)
		}
		_, _, finalDrift := replay.MaxComponentDrift(ref, got)
		return finalDrift
	}

	dmax := run(qmax)
	if dmax != drift.BoundRaw(qmax, n, dt) || dmax > capRaw {
		t.Fatalf("回代失败: Qmax 最坏漂移=%d bound=%d cap=%d",
			dmax, drift.BoundRaw(qmax, n, dt), capRaw)
	}
	if dOver := run(qmax + 1); dOver <= capRaw {
		t.Fatalf("Qmax+1=%d 的最坏漂移 %d 竟未越界，反解不是最大步长", qmax+1, dOver)
	}
}

// TestJSONLRoundTrip 验证 v1/v2 日志写出去再读回来，指令逐字段一致，
// 且 v2 读回的 raw 值就是量化后的值。
func TestJSONLRoundTrip(t *testing.T) {
	opt := api.Options{Bodies: 2, SubSteps: 2, Gravity: 1}
	frames := []replay.Frame{{Commands: []api.Command{
		{Tick: 0, Body: 0, PushX: 16384, PushY: -8192},
		{Tick: 0, Body: 1, PushX: 12345, PushY: -6789},
	}}}
	var v1 bytes.Buffer
	if _, err := replay.WriteJSONL(&v1, opt, frames); err != nil {
		t.Fatal(err)
	}
	l1, ver, _, err := replay.ReadJSONL(bytes.NewReader(v1.Bytes()))
	if err != nil || ver != replay.FormatV1 {
		t.Fatalf("read v1: %v ver=%d", err, ver)
	}
	if len(l1.Frames[0].Commands) != 2 || l1.Frames[0].Commands[1].PushX != 12345 {
		t.Fatalf("v1 往返不一致: %+v", l1.Frames)
	}

	q := replay.QuantizeLog(replay.Log{Options: opt, Frames: frames}, 8)
	var v2 bytes.Buffer
	if _, err := replay.WriteQuantizedJSONL(&v2, opt, q.Frames, 8, 256); err != nil {
		t.Fatal(err)
	}
	l2, ver, bits, err := replay.ReadJSONL(bytes.NewReader(v2.Bytes()))
	if err != nil || ver != replay.FormatV2 || bits != 8 {
		t.Fatalf("read v2: %v ver=%d bits=%d", err, ver, bits)
	}
	if l2.Frames[0].Commands[1].PushX != q.Frames[0].Commands[1].PushX {
		t.Fatalf("v2 档位值未正确乘回: got %d want %d",
			l2.Frames[0].Commands[1].PushX, q.Frames[0].Commands[1].PushX)
	}
}

// deterministicFrames 生成不依赖随机数的多实体指令流，每实体每帧恰好一条。
func deterministicFrames(n, b int) []replay.Frame {
	frames := make([]replay.Frame, n)
	for f := 0; f < n; f++ {
		cmds := make([]api.Command, b)
		for body := 0; body < b; body++ {
			h := mixTest(uint64(f)*1000003 + uint64(body)*9176 + 0x9e3779b97f4a7c15)
			px := 12288 + int(h%8192) + (f-n/2)*128
			py := int((h>>17)%16384) - 8192
			cmds[body] = api.Command{Tick: f, Body: body, PushX: px, PushY: py}
		}
		frames[f] = replay.Frame{Commands: cmds}
	}
	return frames
}

func mixTest(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
