package sim_test

import (
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

// TestIntegrateKeepsSingleSubstepSemantics 锁定向后兼容行为：
// Integrate(sub) 一次仍是一个子步；像旧 api 那样每帧连续调 sub 次后，
// 重力冲量严格等于设定值，且第一次调用的增量等于旧的整除商 q。
func TestIntegrateKeepsSingleSubstepSemantics(t *testing.T) {
	const g = 2
	G := int64(g) << 16
	for _, S := range []int{3, 5, 7} {
		st := sim.NewState(1, fixed.FromInt(g))
		q := G / int64(S)

		before := st.Raw()[3] // velY body0
		st.Integrate(S)
		afterFirst := st.Raw()[3]
		if d := int64(afterFirst - before); d != q {
			t.Fatalf("S=%d 首次 Integrate 增量=%d，应与旧整除商 q=%d 一致", S, d, q)
		}

		for k := 1; k < S; k++ {
			st.Integrate(S)
		}
		if got := st.LastFrameGravityImpulse(); got != G {
			t.Fatalf("S=%d 连续 %d 次后帧冲量=%d 期望 %d", S, S, got, G)
		}
		if got := st.TotalGravityImpulse(); got != G {
			t.Fatalf("S=%d 首帧累计=%d 期望 %d", S, got, G)
		}

		// 再来一帧：相位必须回到 0 并重新守恒。
		for k := 0; k < S; k++ {
			st.Integrate(S)
		}
		if got := st.LastFrameGravityImpulse(); got != G {
			t.Fatalf("S=%d 第二帧帧冲量=%d 期望 %d", S, got, G)
		}
		if got := st.TotalGravityImpulse(); got != 2*G {
			t.Fatalf("S=%d 两帧累计=%d 期望 %d", S, got, 2*G)
		}
	}
}

// TestGravityScheduleSumsExactly 直接验证分摊表元素之和严格等于重力。
func TestGravityScheduleSumsExactly(t *testing.T) {
	for _, g := range []int{0, 1, 2, 10} {
		rawG := fixed.FromInt(g).Raw()
		for S := 1; S <= 16; S++ {
			sched := sim.GravitySchedule(rawG, S)
			var sum int64
			for _, v := range sched {
				sum += int64(v.Raw())
			}
			if sum != int64(rawG) || len(sched) != S {
				t.Fatalf("g=%d S=%d schedule len=%d sum=%d G=%d", g, S, len(sched), sum, rawG)
			}
		}
	}
}
