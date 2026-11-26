package main

import (
	"testing"

	"goframe/internal/hash"
	"goframe/internal/sim"
)

// TestCrossCheckLabVersusPackage 保证 cmd/hashlab 内部 FNV 与 internal/hash 正式实现逐位一致，
// 避免实验台与产品代码各算各的。
func TestCrossCheckLabVersusPackage(t *testing.T) {
	s := sim.NewState(2, 0)
	s.Push(0, 1<<14, 0)
	s.Integrate(4)
	s.Push(1, -(1 << 12), 1<<10)
	s.Integrate(2)
	snap := s.Raw()

	for tick := 0; tick <= 5; tick++ {
		if got, want := v2Hash(tick, snap), hash.State(tick, s); got != want {
			t.Fatalf("tick=%d v2 不一致: lab=%016x pkg=%016x", tick, got, want)
		}
		for _, b := range []uint{4, 8, 12, 16} {
			if got, want := dropLowHash(b)(tick, snap), hash.StateDropLow(tick, s, b); got != want {
				t.Fatalf("tick=%d b=%d drop-low 不一致: lab=%016x pkg=%016x", tick, b, got, want)
			}
		}
		for _, k := range []uint{8, 16, 17, 24} {
			if got, want := keepLowHash(k)(tick, snap), hash.StateKeepLow(tick, s, k); got != want {
				t.Fatalf("tick=%d k=%d keep-low 不一致: lab=%016x pkg=%016x", tick, k, got, want)
			}
		}
	}
}

// TestProbabilityIdentityDropLow 穷举验证分桶模型的闭式漏判概率，
// 并专门覆盖旧产物公式 1-Δ/B 在 Δ≥B 时失效的情形（第三组 Δ=196608）。
func TestProbabilityIdentityDropLow(t *testing.T) {
	cases := []struct{ delta, b int }{
		{1, 4}, {1, 8}, {1, 12}, {1, 16},
		{64, 4}, {64, 8}, {64, 12}, {64, 16},
		{192, 4}, {192, 8}, {192, 12}, {192, 16},
	}
	for _, c := range cases {
		theory := missCountDropLow(c.delta, c.b)
		emp := empiricalMissDropLow(c.delta, c.b)
		if theory != emp {
			t.Fatalf("Δ=%d b=%d: 理论漏判 %d != 实测 %d", c.delta, c.b, theory, emp)
		}
		B := 1 << c.b
		if got := probDropLow(c.delta, c.b); got != float64(theory)/float64(B) {
			t.Fatalf("Δ=%d b=%d 概率函数错误: %v", c.delta, c.b, got)
		}
	}

	// 钉死 Δ≥B 的行为：差一个完整桶宽必然落在不同桶，漏判率为 0（不是 1，也不是负值）。
	if got := probDropLow(64, 4); got != 0.0 {
		t.Fatalf("Δ=64,b=4（Δ=B）漏判率应为 0，得到 %v", got)
	}
	if got := probDropLow(3*65536, 8); got != 0.0 {
		t.Fatalf("Δ=3*65536,b=8（Δ≫B）漏判率应为 0，得到 %v", got)
	}
	// 桶内小差异：Δ=192,b=8 时 B-Δ=64 个对齐漏判，概率 1/4。
	if got := probDropLow(192, 8); got != 0.25 {
		t.Fatalf("Δ=192,b=8 漏判率应为 0.25，得到 %v", got)
	}
}

// TestProbabilityIdentityKeepLow 验证“只取低位”模型：漏判 iff 2^k|Δ，与对齐无关。
func TestProbabilityIdentityKeepLow(t *testing.T) {
	for m := 1; m <= 3; m++ {
		d := m * 65536
		for _, k := range []int{8, 16, 17, 24} {
			theory := missCountKeepLow(d, k)
			emp := empiricalMissKeepLow(d, k)
			if theory != emp {
				t.Fatalf("Δ=%d k=%d: 理论 %d != 实测 %d", d, k, theory, emp)
			}
			want := 0
			if d%(1<<k) == 0 {
				want = 1 << k
			}
			if theory != want {
				t.Fatalf("Δ=%d k=%d: 整除判据错误，got %d want %d", d, k, theory, want)
			}
		}
	}
}

// TestForkFrames 验证三组真实分叉世界：首差帧恒为 4，v1 漏判，v2 在 tick=4 判分叉。
func TestForkFrames(t *testing.T) {
	for _, d := range groupDeltas {
		rows, err := trace(d, modeStable, 8, dropLowHash(headlineB), v2Hash)
		if err != nil {
			t.Fatal(err)
		}
		if got := firstTrueDiffTick(rows); got != forkTick {
			t.Fatalf("Δ=%d 实际首差帧=%d，期望 %d", d, got, forkTick)
		}
		if got := firstJudgedTick(rows, false); got != -1 {
			t.Fatalf("Δ=%d v1 应全程漏判，却在 tick=%d 判分叉", d, got)
		}
		if got := firstJudgedTick(rows, true); got != forkTick {
			t.Fatalf("Δ=%d v2 应在 tick=%d 判分叉，得到 %d", d, forkTick, got)
		}
	}
}

// TestGrowingDiffFrameDelay 验证位宽越窄，增长差的判定帧越延后，v2 恒定为首差帧；
// 同时用 growingFirstJudgeTick 的解析预测交叉核对真实仿真的判定帧。
func TestGrowingDiffFrameDelay(t *testing.T) {
	const window = 80
	for _, d := range groupDeltas {
		prev := -1
		for _, b := range sweepB {
			rows, err := trace(d, modeGrowing, window, dropLowHash(uint(b)), v2Hash)
			if err != nil {
				t.Fatal(err)
			}
			judged := firstJudgedTick(rows, false)
			if b > 4 && judged != -1 && prev != -1 && judged < prev {
				t.Fatalf("Δ=%d b=%d 判定帧 %d 早于更宽桶的 %d，单调性错误", d, b, judged, prev)
			}
			prev = judged

			// 解析预测：t 是首差帧之后的偏移，全局帧号 = forkTick + t - 1。
			analyticT := growingFirstJudgeTick(d, b, window)
			wantTick := -1
			if analyticT > 0 {
				wantTick = forkTick + analyticT - 1
			}
			if judged != wantTick {
				t.Fatalf("Δ=%d b=%d 仿真判定帧 %d 与解析预测 %d 不符", d, b, judged, wantTick)
			}
			if got := firstJudgedTick(rows, true); got != forkTick {
				t.Fatalf("Δ=%d v2 增长差判定帧=%d，期望 %d", d, got, forkTick)
			}
		}
	}
}

// TestBuildForkedWorldsSanity 直接检查构造出的世界快照确实不同且差值正确。
func TestBuildForkedWorldsSanity(t *testing.T) {
	for _, d := range groupDeltas {
		a, b, err := buildForkedWorlds(d, modeStable)
		if err != nil {
			t.Fatal(err)
		}
		sa, sb := a.Snapshot(), b.Snapshot()
		if sa[0] != 0 || sb[0] != int32(d) {
			t.Fatalf("Δ=%d 稳定差位置错误: a=%d b=%d", d, sa[0], sb[0])
		}
		if sa[2] != sb[2] { // 速度字段应已被反向冲量抵消
			t.Fatalf("Δ=%d 稳定差速度未抵消: %d vs %d", d, sa[2], sb[2])
		}
	}
}
