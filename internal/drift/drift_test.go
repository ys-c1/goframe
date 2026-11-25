package drift

import "testing"

func TestWeightSum(t *testing.T) {
	// dt*N(N+1)/2
	if got := WeightSum(20, 4); got != 840 {
		t.Fatalf("WeightSum(20,4)=%d, want 840", got)
	}
	if got := WeightSum(1, 1); got != 1 {
		t.Fatalf("WeightSum(1,1)=%d, want 1", got)
	}
}

func TestBoundQuadraticInFramesLinearInDtAndQ(t *testing.T) {
	base := BoundRaw(256, 20, 4) // 128*840=107520
	if base != 107520 {
		t.Fatalf("base=%d want 107520", base)
	}
	// N 翻倍（20->40），N(N+1) 项约变 3280/840≈3.905 倍，精确等于权重比。
	if got := BoundRaw(256, 40, 4); got != 128*3280 {
		t.Fatalf("N=40 bound=%d", got)
	}
	// dt 翻倍，界精确翻倍。
	if got := BoundRaw(256, 20, 8); got != 2*base {
		t.Fatalf("dt=8 bound=%d want %d", got, 2*base)
	}
	// Q 翻四倍（256->1024），Emax 翻四倍，界翻四倍。
	if got := BoundRaw(1024, 20, 4); got != 4*base {
		t.Fatalf("Q=1024 bound=%d want %d", got, 4*base)
	}
}

func TestInversionRoundTrip(t *testing.T) {
	const cap = int64(8) << 16 // 8 单位
	const n, dt = 20, 4
	qc := MaxContinuousStepRaw(float64(cap), n, dt)
	w := WeightSum(n, dt)
	// 连续式回代：(qc/2)*W 必须恰好回到 cap。
	back := qc / 2 * float64(w)
	if d := back - float64(cap); d > 1 || d < -1 {
		t.Fatalf("连续反解回代偏差 %v", d)
	}
	// 整数最大步长：自身满足，Q+1 不满足（真正“最大”）。
	q := MaxIntegerStepRaw(cap, n, dt)
	if BoundRaw(q, n, dt) > cap {
		t.Fatalf("Qmax=%d 自身越界", q)
	}
	if BoundRaw(q+1, n, dt) <= cap {
		t.Fatalf("Qmax=%d 不是最大整数步长，Q+1=%d 仍满足", q, q+1)
	}
	// 本任务参数下推荐的 2 的幂档是 1024 raw = 6 定点位，
	// 且目标 8 定点位（Q=256）在限内。
	q2 := MaxPowerOfTwoStepRaw(cap, n, dt)
	if q2 != 1024 {
		t.Fatalf("power2 Q=%d want 1024", q2)
	}
	if bits := FracBitsForStepRaw(q2); bits != 6 {
		t.Fatalf("recommended bits=%d want 6", bits)
	}
	if BoundRaw(256, n, dt) > cap {
		t.Fatal("8 定点位档不应越界")
	}
}

func TestFracBits(t *testing.T) {
	if FracBitsForStepRaw(256) != 8 {
		t.Fatal("256 应为 8 位")
	}
	if FracBitsForStepRaw(1249) != -1 {
		t.Fatal("非 2 的幂应返回 -1")
	}
}
