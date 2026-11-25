package clock

import (
	"fmt"
	"testing"

	"goframe/api"
)

// 三档幅度都必须复现：墙钟帧号非单调，且累计回退偏差恰好等于回拨幅度 R。
func TestWallRollbackNonMonotonicAndDeviationEqualsR(t *testing.T) {
	for _, r := range Levels {
		s, err := Run(Config{RollbackFrames: r, Slots: 120, RollbackAt: 105, PeriodMS: DefaultPeriodMS})
		if err != nil {
			t.Fatalf("R=%d Run: %v", r, err)
		}
		if !s.WallProofOK {
			t.Fatalf("R=%d 墙钟命题未成立: nonMonoJump=%d D_W=%d (期望 %d)",
				r, s.WallMaxBackwardJump, s.WallCumRegression, r)
		}
		if s.FrameRollbackPre-s.FrameRollbackPost != int64(r) {
			t.Fatalf("R=%d 回拨瞬间帧号跌幅 = %d, 期望 %d",
				r, s.FrameRollbackPre-s.FrameRollbackPost, r)
		}
		// 时间轴里必须真的存在负的帧号增量（非单调的直接证据）。
		sawNegative := false
		for _, p := range s.Points {
			if p.WallDelta < 0 {
				sawNegative = true
			}
		}
		if !sawNegative {
			t.Fatalf("R=%d 时间轴中没有负的墙钟帧号增量", r)
		}
	}
}

// 一般命题：多次回拨时，累计偏差等于各次回拨幅度之和 ΣR_j。
func TestCumulativeDeviationIsSumOfRollbackMagnitudes(t *testing.T) {
	cases := [][]int{
		{1},
		{10},
		{100},
		{1, 10, 100},
		{3, 7},
		{5, 5, 5},
		{2, 2, 2, 2},
	}
	for _, rolls := range cases {
		var want int64
		cur := int64(0)
		seq := []int64{0}
		for j, r := range rolls {
			// 两次回拨之间正常推进若干帧（+1）。
			for k := 0; k < 2+j%3; k++ {
				cur++
				seq = append(seq, cur)
			}
			cur -= int64(r) // 回拨事件：帧号直接下跳 r
			seq = append(seq, cur)
			want += int64(r)
		}
		if got := Regression(seq); got != want {
			t.Fatalf("rolls=%v 累计偏差=%d, 期望 ΣR=%d; seq=%v", rolls, got, want, seq)
		}
		if got := Regression(seq); got != want {
			t.Fatalf("rolls=%v 最大回退量校验失败", rolls)
		}
	}
}

// 修复方案：任何一档回拨下，自增帧号的累计回退偏差都必须为 0（< 1 帧）。
func TestCounterFixHasZeroDeviationForAllLevels(t *testing.T) {
	for _, r := range Levels {
		s, err := Run(Config{RollbackFrames: r, Slots: 120, RollbackAt: 105, PeriodMS: DefaultPeriodMS})
		if err != nil {
			t.Fatal(err)
		}
		if s.FixedCumRegression != 0 {
			t.Fatalf("R=%d 修复后累计偏差=%d, 必须为 0 (<1 帧)", r, s.FixedCumRegression)
		}
		if !s.FixPass || !s.OK {
			t.Fatalf("R=%d 验收未通过: fixPass=%v ok=%v", r, s.FixPass, s.OK)
		}
		for i, p := range s.Points {
			if p.FixedDelta < 0 {
				t.Fatalf("R=%d 第 %d 行自增帧号回退: delta=%d", r, i, p.FixedDelta)
			}
			if p.FixedFrame != p.IdealFrame {
				t.Fatalf("R=%d 第 %d 行 fixed=%d ideal=%d", r, i, p.FixedFrame, p.IdealFrame)
			}
		}
	}
}

// Counter.Poll 在任意轮询节奏下都给出 floor(t/Δ)，且提交帧号严格 0,1,2,...
func TestCounterPollMonotonicAndBound(t *testing.T) {
	const p = int64(10)
	c := &Counter{}
	var committed []int64
	// 故意用不均匀、偶尔一次跨多帧的单调采样时刻。
	times := []int64{0, 3, 10, 12, 20, 39, 40, 41, 70, 70, 105}
	for _, ms := range times {
		made, err := c.Poll(ms, 0, p, func(frame int64) error {
			committed = append(committed, frame)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		wantTarget := FrameAt(ms, 0, p)
		if c.Frame() != wantTarget {
			t.Fatalf("ms=%d counter=%d target=%d made=%d", ms, c.Frame(), wantTarget, made)
		}
		rem := LagRemainder(ms, 0, p)
		if rem < 0 || rem >= p {
			t.Fatalf("ms=%d 滞后余量 %d 不在 [0,%d)", ms, rem, p)
		}
	}
	for i, f := range committed {
		if f != int64(i) {
			t.Fatalf("提交帧号序列不是严格自增: committed[%d]=%d", i, f)
		}
	}
	if d := Regression(committed); d != 0 {
		t.Fatalf("提交序列累计回退偏差=%d, 期望 0", d)
	}
}

// 量化上界：对所有余数相位，0 ≤ lag/Δ < 1。
func TestLagBoundBelowOneFrameForAllPhases(t *testing.T) {
	const p = int64(10)
	for ms := int64(-50); ms <= 200; ms++ {
		rem := LagRemainder(ms, 0, p)
		if rem < 0 || rem >= p {
			t.Fatalf("ms=%d rem=%d 越界 [0,%d)", ms, rem, p)
		}
	}
}

// 钟差三点与上界：回拨前 0，回拨瞬间跳到 RΔ，回拨后保持 RΔ；
// 整条时间轴 0 ≤ mono-wall ≤ RΔ，帧号差 ≤ R（同相位精确等于欠账）。
func TestClockDifferenceThreePointsAndBounds(t *testing.T) {
	for _, r := range Levels {
		s, err := Run(Config{RollbackFrames: r, Slots: 120, RollbackAt: 105, PeriodMS: DefaultPeriodMS})
		if err != nil {
			t.Fatal(err)
		}
		R := int64(r)
		wantMS := R * DefaultPeriodMS
		want := [3]int64{0, wantMS, wantMS}
		for i, dp := range s.DiffPoints {
			if dp.DiffMS != want[i] {
				t.Fatalf("R=%d 第 %d 个钟差点=%dms, 期望 %dms", r, i, dp.DiffMS, want[i])
			}
		}
		if s.DiffPoints[1].DiffFrames != R || s.DiffPoints[2].DiffFrames != R {
			t.Fatalf("R=%d 钟差折算帧数不为 R: %+v", r, s.DiffPoints)
		}
		for _, p := range s.Points {
			if p.GapMS < 0 || p.GapMS > wantMS {
				t.Fatalf("R=%d 钟差 %dms 超出 [0,%d]ms", r, p.GapMS, wantMS)
			}
			gapFrames := p.FixedFrame - p.WallFrame
			if gapFrames < 0 || gapFrames > R {
				t.Fatalf("R=%d 帧号差 %d 超出 [0,%d]", r, gapFrames, R)
			}
			if gapFrames != p.CatchupDebtFrames {
				t.Fatalf("R=%d 帧号差=%d 与追帧欠账=%d 不一致", r, gapFrames, p.CatchupDebtFrames)
			}
		}
	}
}

// 负数墙钟读数也要能正确向下取整（R=100 且回拨点较早时会出现负帧号）。
func TestFloorDivNegative(t *testing.T) {
	cases := [][3]int64{
		{0, 10, 0}, {9, 10, 0}, {10, 10, 1}, {-1, 10, -1},
		{-10, 10, -1}, {-11, 10, -2}, {-101, 10, -11},
	}
	for _, c := range cases {
		if got := FrameAt(c[0], 0, c[1]); got != c[2] {
			t.Fatalf("floor(%d/%d)=%d, 期望 %d", c[0], c[1], got, c[2])
		}
	}
}

// 端到端：真实 api.World 由单调钟 + Counter 驱动，墙钟在中途回拨三档幅度，
// World 的 tick 与状态指纹必须完全不受影响，且三组结果与无回拨基线一致。
func TestWorldUnaffectedByWallRollback(t *testing.T) {
	const frames = 32
	baseline := runWorldWithWallRollback(t, 0, -1, frames)
	for _, r := range Levels {
		got := runWorldWithWallRollback(t, r, 10, frames)
		if len(got) != len(baseline) {
			t.Fatalf("R=%d 指纹长度 %d != 基线 %d", r, len(got), len(baseline))
		}
		for i := range baseline {
			if got[i] != baseline[i] {
				t.Fatalf("R=%d tick=%d 指纹受墙钟回拨影响: %016x != %016x",
					r, i, got[i], baseline[i])
			}
		}
	}
}

// runWorldWithWallRollback 用“单调钟截止时间 + 帧号自增”驱动真实 World；
// rollbackR>0 时在 rb 槽把墙钟回拨 rollbackR 帧。返回每帧指纹。
func runWorldWithWallRollback(t *testing.T, rollbackR, rb, frames int) []uint64 {
	t.Helper()
	w, err := api.New(api.Options{Bodies: 4, SubSteps: 2, Gravity: 1})
	if err != nil {
		t.Fatal(err)
	}
	wall := NewWallClock(0)
	mono := NewMonoClock(0)
	counter := &Counter{}
	var hashes []uint64

	for slot := 0; slot <= frames; slot++ {
		if slot > 0 {
			if err := mono.AdvanceMS(DefaultPeriodMS); err != nil {
				t.Fatal(err)
			}
			if err := wall.AdvanceMS(DefaultPeriodMS); err != nil {
				t.Fatal(err)
			}
		}
		if rollbackR > 0 && slot == rb {
			if err := wall.RollbackFrames(rollbackR, DefaultPeriodMS); err != nil {
				t.Fatal(err)
			}
			if mono.NowMS() == wall.NowMS() {
				t.Fatal("回拨后墙钟应当落后于单调钟")
			}
		}
		_, err := counter.Poll(mono.NowMS(), 0, DefaultPeriodMS, func(frame int64) error {
			cmds := []api.Command{{
				Tick:  int(frame),
				Body:  int(frame) % 4,
				PushX: 1 << 14,
			}}
			if err := w.Step(cmds); err != nil {
				return err
			}
			if w.Tick() != int(frame)+1 {
				return fmt.Errorf("frame=%d 提交后 World.Tick=%d", frame, w.Tick())
			}
			hashes = append(hashes, w.Hash())
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if counter.Frame() != int64(frames) {
		t.Fatalf("提交帧数=%d, 期望 %d", counter.Frame(), frames)
	}
	return hashes
}
