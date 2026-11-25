package verify

import (
	"testing"

	"goframe/api"
	"goframe/replay"
)

const (
	testBodies  = 8
	testGravity = 2
	testFrames  = 24
)

// 任务硬性要求：子步数 1/2/4 三档，同一帧末态哈希必须完全相同，差一帧都不过。
func TestInvariance_One_Two_Four_SameFrameHashes(t *testing.T) {
	logs := map[string][]replay.Frame{
		"canonical": CanonicalFrames(testFrames, testBodies),
		"rich":      RichFrames(20261126, testFrames, testBodies),
	}
	for name, frames := range logs {
		m, runs, err := BuildMatrix(testBodies, testGravity, frames)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !m.SameFrame124 {
			t.Fatalf("%s: 1/2/4 档存在不同帧哈希: %+v", name, m.Pair1v4.DivergentTicks)
		}
		// 显式逐帧钉死 1、2、4。
		for f := 0; f < testFrames; f++ {
			h1, h2, h4 := runs[1].Hashes[f], runs[2].Hashes[f], runs[4].Hashes[f]
			if h1 != h2 || h1 != h4 {
				t.Fatalf("%s tick=%d 哈希不一致: sub1=%016x sub2=%016x sub4=%016x", name, f, h1, h2, h4)
			}
		}
	}
}

// 单独验这一条：两档子步数（取题面的 1 与 4）末态逐帧比对，哈希差值必须为零。
func TestFrameByFrame_HashDifferenceZero_TwoTiers(t *testing.T) {
	frames := CanonicalFrames(testFrames, testBodies)
	r1, err := Run(testBodies, 1, testGravity, frames)
	if err != nil {
		t.Fatal(err)
	}
	r4, err := Run(testBodies, 4, testGravity, frames)
	if err != nil {
		t.Fatal(err)
	}
	pair := ComparePair(1, 4, r1, r4)
	if len(pair.Frames) != testFrames {
		t.Fatalf("比对帧数=%d, 期望 %d", len(pair.Frames), testFrames)
	}
	for _, fd := range pair.Frames {
		if fd.HashDelta != 0 {
			t.Fatalf("tick=%d 哈希差值非零: delta=%d (%s vs %s)", fd.Tick, fd.HashDelta, fd.HashA, fd.HashB)
		}
		if fd.HashXOR != "0x0000000000000000" {
			t.Fatalf("tick=%d 哈希异或非零: %s", fd.Tick, fd.HashXOR)
		}
	}
	if !pair.AllDeltaZero || !pair.AllXORZero || !pair.AllHashEqual {
		t.Fatalf("逐帧哈希差值为零检查未通过: %+v", pair)
	}
}

// 边界档：sub=8 也必须与 sub=1 逐帧完全相同。
func TestSubStep8_Boundary_Identical(t *testing.T) {
	frames := RichFrames(777, testFrames, testBodies)
	r1, err := Run(testBodies, 1, testGravity, frames)
	if err != nil {
		t.Fatal(err)
	}
	r8, err := Run(testBodies, 8, testGravity, frames)
	if err != nil {
		t.Fatal(err)
	}
	pair := ComparePair(1, 8, r1, r8)
	if !pair.AllHashEqual || !pair.AllDeltaZero {
		t.Fatalf("sub=8 边界档与 sub=1 不一致，分歧帧=%v", pair.DivergentTicks)
	}
}

// 更强：不仅哈希，导出的原始状态切片也要逐分量相同。
func TestAllTiers_RawSnapshotsIdentical(t *testing.T) {
	frames := RichFrames(424242, 40, 4)
	if _, runs, err := BuildMatrix(4, 3, frames); err != nil {
		t.Fatal(err)
	} else {
		for f := range frames {
			ref := runs[1].Raw[f]
			for _, n := range []int{2, 4, 8} {
				if !equalI32(ref, runs[n].Raw[f]) {
					t.Fatalf("frame=%d sub=%d 原始状态不同", f, n)
				}
			}
		}
	}
}

// 证明旧 Euler 与子步数相关，且重力偏差正是 G*h^2 二阶分裂项的整数三角倍。
func TestEuler_Dependence_And_GravityH2Term(t *testing.T) {
	frames := CanonicalFrames(20, testBodies)
	p, err := BuildEulerProof(testGravity, frames)
	if err != nil {
		t.Fatal(err)
	}
	const G = int64(testGravity)
	const q16 = int64(1 << 16)

	// 逐字旧代码、静止一帧：y1=G, y4=5G/2（Q16 原始值，G=2 时为 131072/327680）。
	if want := G * q16; int64(p.LiteralYRaw[1]) != want {
		t.Fatalf("literal y1 raw=%d want %d", p.LiteralYRaw[1], want)
	}
	if want := G * 5 * q16 / 2; int64(p.LiteralYRaw[4]) != want {
		t.Fatalf("literal y4 raw=%d want %d", p.LiteralYRaw[4], want)
	}
	// 1minus4 = -3G/2。
	if want := -(3 * G * q16 / 2); p.LiteralGap1v4Raw != want {
		t.Fatalf("literal gap raw=%d want %d", p.LiteralGap1v4Raw, want)
	}
	if !p.CanonicalOldHashesDiffer || p.CanonicalOldEqualFrames != 0 {
		t.Fatalf("旧 Euler 规范流 1/4 哈希应全部不同: equal=%d/%d", p.CanonicalOldEqualFrames, p.CanonicalFrameCount)
	}

	// h 正确的半隐式 Euler：1 档与 4 档重力位移差 = 3G/8。
	// 而 6*(G*h4^2) = 6*G/16 = 3G/8，精确相等 —— 即 G 乘子步长二阶项的 6 倍。
	const n4 = int64(4)
	h4Sq := q16 / (n4 * n4)          // h4^2 的 Q16 表示 = 1/16
	gH4SqRaw := G * q16 * h4Sq / q16 // G * h4^2，回到 Q16 raw 尺度
	gapRaw := 3 * G * q16 / 8        // 3G/8 的 Q16 raw
	if 6*gH4SqRaw != gapRaw {
		t.Fatalf("二阶项恒等式不成立: 6*G*h4^2 raw=%d vs gap=%d", 6*gH4SqRaw, gapRaw)
	}
	if p.CoefficientOnH4Sq != 6 {
		t.Fatalf("三角系数=%d want 6", p.CoefficientOnH4Sq)
	}
}

// 回放记录：Record 与 Playback 的逐帧指纹必须一致。
func TestReplay_RecordAndPlaybackIdentical(t *testing.T) {
	frames := CanonicalFrames(12, 3)
	a, err := BuildReplayArtifact(api.Options{Bodies: 3, SubSteps: 4, Gravity: 2}, frames)
	if err != nil {
		t.Fatal(err)
	}
	if !a.ReplayIdentical {
		t.Fatal("回放指纹与记录不一致")
	}
	if len(a.RecordedHashes) != 12 || len(a.PlaybackHashes) != 12 {
		t.Fatalf("回放帧数异常: rec=%d play=%d", len(a.RecordedHashes), len(a.PlaybackHashes))
	}
}
