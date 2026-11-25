package projectile

import (
	"math/big"
	"testing"
)

func ratEqual(t *testing.T, got, want *big.Rat, what string) {
	t.Helper()
	if got.Cmp(want) != 0 {
		t.Fatalf("%s = %s, 期望 %s", what, got.RatString(), want.RatString())
	}
}

func TestQuantizationTiers(t *testing.T) {
	cases := []struct {
		bits   int
		num    int64
		digits int
		dg     string
		coeff  string
	}{
		{8, 2509, 4, "1/1280", "1/2560"},
		{16, 642253, 6, "1/327680", "1/655360"},
		{32, 42090679501, 11, "1/21474836480", "1/42949672960"},
	}
	for _, c := range cases {
		tier := Tier{Bits: c.bits}
		if tier.Numerator() != c.num {
			t.Fatalf("f=%d numerator=%d 期望 %d", c.bits, tier.Numerator(), c.num)
		}
		if tier.Digits() != c.digits {
			t.Fatalf("f=%d digits=%d 期望 %d", c.bits, tier.Digits(), c.digits)
		}
		if tier.DGravity().RatString() != c.dg {
			t.Fatalf("f=%d δg=%s 期望 %s", c.bits, tier.DGravity().RatString(), c.dg)
		}
		if tier.AbsCoeff().RatString() != c.coeff {
			t.Fatalf("f=%d c=%s 期望 %s", c.bits, tier.AbsCoeff().RatString(), c.coeff)
		}
	}
}

// TestRawRecurrenceMatchesQuadraticLaw 独立地跑整数递推，
// 核对每帧位置误差严格等于 δg·n(n+1)/2（即二次增长，n² 系数 δg/2）。
func TestRawRecurrenceMatchesQuadraticLaw(t *testing.T) {
	tr := Trajectory{ID: "X", H0: 100, VX0: 900, VY0: 300}
	for _, tier := range DefaultTiers() {
		s := tier.Scale()
		x, y := int64(0), tr.H0*s
		vx, vy := tr.VX0*s, tr.VY0*s
		for n := int64(1); n <= 50; n++ {
			vy -= tier.Numerator()
			x += vx
			y += vy

			// 定点整数式：y_raw = S(H0+VY0 n) − num·n(n+1)/2
			wantRaw := s*(tr.H0+tr.VY0*n) - tier.Numerator()*n*(n+1)/2
			if y != wantRaw {
				t.Fatalf("%s n=%d y_raw=%d 与整数闭式 %d 不符", tier.Name, n, y, wantRaw)
			}
			if x != tr.VX0*s*n {
				t.Fatalf("%s n=%d x_raw=%d 发生了不应有的水平漂移", tier.Name, n, x)
			}

			// 与精确参考的同帧高度差（世界单位，绝对值）。
			gotDy := new(big.Rat).Abs(new(big.Rat).Sub(
				big.NewRat(y, s), tr.RefY(n)))
			wantDy := tier.ErrAtFrame(n)
			ratEqual(t, gotDy, wantDy, tier.Name+" Δy")
		}
	}
}

func TestBreachFramesAndBoundary(t *testing.T) {
	eps := big.NewRat(1, 10)
	want := map[int]int64{8: 16, 16: 256, 32: 65536}
	for _, tier := range DefaultTiers() {
		k := tier.BreachFrame(eps)
		if k != want[tier.Bits] {
			t.Fatalf("%s breach=%d 期望 %d", tier.Name, k, want[tier.Bits])
		}
		if tier.ErrAtFrame(k-1).Cmp(eps) > 0 {
			t.Fatalf("%s k-1=%d 已越界，首次越界帧应更早", tier.Name, k-1)
		}
		if tier.ErrAtFrame(k).Cmp(eps) <= 0 {
			t.Fatalf("%s k=%d 仍在容差内，不是首次越界", tier.Name, k)
		}
	}

	// Q16.16 在 ε=0.1 下边界两帧的精确值，正好对称夹住容差。
	q16 := Tier{Bits: 16}
	ratEqual(t, q16.ErrAtFrame(255), big.NewRat(65280, 655360), "err@255")
	ratEqual(t, q16.ErrAtFrame(256), big.NewRat(65792, 655360), "err@256")
}

func TestLandingFrames(t *testing.T) {
	trajs := DefaultTrajectories()
	if got := trajs[0].ReferenceLanding().Frame; got != 61 {
		t.Fatalf("A 参考落地帧=%d 期望 61", got)
	}
	if got := trajs[1].ReferenceLanding().Frame; got != 408 {
		t.Fatalf("B 参考落地帧=%d 期望 408", got)
	}
}

func TestPassMatrixAndConsistency(t *testing.T) {
	eps := big.NewRat(1, 10)
	rows := AnalyzeAll(eps, nil, nil)
	if len(rows) != 6 {
		t.Fatalf("应有 2 轨迹 × 3 档 = 6 行，实际 %d", len(rows))
	}
	// 行序：轨迹 A 的三档，再轨迹 B 的三档。
	wantPass := []bool{false, true, true, false, false, true}
	for i, r := range rows {
		if r.PassAtLanding != wantPass[i] {
			t.Fatalf("行 %d (%s/%s) pass=%v 期望 %v，落点差=%.6g",
				i, r.TrajID, r.Tier, r.PassAtLanding, wantPass[i], r.LandDiffY)
		}
		// 容差核对：落地通过 ⇔ 参考落地帧严格早于首次越界帧。
		if r.PassAtLanding != (r.RefLandFrame < r.BreachFrame) {
			t.Fatalf("%s/%s 判定与越界帧不一致: pass=%v nRef=%d k=%d",
				r.TrajID, r.Tier, r.PassAtLanding, r.RefLandFrame, r.BreachFrame)
		}
		if r.BreachBeforeLanding != (r.BreachFrame <= r.RefLandFrame) {
			t.Fatalf("%s/%s BreachBeforeLanding 自相矛盾", r.TrajID, r.Tier)
		}
	}
}

func TestHorizontalOffset(t *testing.T) {
	eps := big.NewRat(1, 10)
	for _, tr := range DefaultTrajectories() {
		rows := make([]Row, 3)
		for i, tier := range DefaultTiers() {
			rows[i] = tr.Analyze(tier, eps)
		}
		for _, r := range rows {
			if r.SameFrameDx != 0 {
				t.Fatalf("%s/%s 同帧横向偏移应为 0（vx 精确、无水平加速）", tr.ID, r.Tier)
			}
			// 三档都向上舍入 → g 偏大 → 更早落地 → 正 vx 下落点偏左，Δx<0。
			if !(r.HorizontalDxRange < 0) {
				t.Fatalf("%s/%s 横向射程差应为负，实际 %.6g", tr.ID, r.Tier, r.HorizontalDxRange)
			}
		}
		if !(absF(rows[0].HorizontalDxRange) > absF(rows[1].HorizontalDxRange) &&
			absF(rows[1].HorizontalDxRange) > absF(rows[2].HorizontalDxRange)) {
			t.Fatalf("%s 横向偏移未随位数升高单调缩小: %g %g %g",
				tr.ID, rows[0].HorizontalDxRange, rows[1].HorizontalDxRange, rows[2].HorizontalDxRange)
		}
	}
}

func TestLandingGapMetric(t *testing.T) {
	// 插值地面落点距 = |ΔX|；其容差判定与“同帧位移差”不同：
	// B/Q16 同帧 Δ=0.2546 已越界，但地面射程距 0.0381 仍 ≤0.1。
	rows := AnalyzeAll(big.NewRat(1, 10), nil, nil)
	wantGapPass := []bool{false, true, true, false, true, true}
	for i, r := range rows {
		if r.LandingGapPass != wantGapPass[i] {
			t.Fatalf("%s/%s LandingGapPass=%v 期望 %v（gap=%.6g）",
				r.TrajID, r.Tier, r.LandingGapPass, wantGapPass[i], r.LandingGap)
		}
		if got := absF(r.HorizontalDxRange); !almostEq(r.LandingGap, got, 1e-12) {
			t.Fatalf("%s/%s LandingGap=%g 应等于 |ΔX|=%g", r.TrajID, r.Tier, r.LandingGap, got)
		}
	}
}

func almostEq(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestTierErrorRatios(t *testing.T) { // 同一参考落地帧处，落点差之比严格等于 δg 之比：
	// Q8 误差 = 256·Q16 误差；Q16 误差 = 65536·Q32 误差。
	nA, nB := int64(61), int64(408)
	for _, n := range []int64{nA, nB} {
		e8 := Tier{Bits: 8}.ErrAtFrame(n)
		e16 := Tier{Bits: 16}.ErrAtFrame(n)
		e32 := Tier{Bits: 32}.ErrAtFrame(n)
		ratEqual(t, new(big.Rat).Mul(e16, big.NewRat(256, 1)), e8, "e8 vs 256 e16")
		ratEqual(t, new(big.Rat).Mul(e32, big.NewRat(65536, 1)), e16, "e16 vs 65536 e32")
	}
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
