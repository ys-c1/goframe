package fixed

import "testing"

// TestMulRoundsFloor 锁定 Mul 的取整方向：算术右移即向下取整（floor）。
// 若有人为了“看起来更直观”改成向零截断或四舍五入，本测试先失败。
func TestMulRoundsFloor(t *testing.T) {
	cases := []struct {
		a, b, want Value
	}{
		{FromRaw(3), One, 3},             // 3.0
		{FromRaw(-3), One, -3},           // -3.0 精确
		{FromRaw(-1), One, -1},           // 精确负数
		{FromRaw(1 << 15), One, 1 << 15}, // 0.5 精确
		// -1.5 * 1.0 = -1.5，原始 -98304；右移精确。
		{FromRaw(-3 << 15), One, -3 << 15},
	}
	for _, c := range cases {
		if got := c.a.Mul(c.b); got != c.want {
			t.Errorf("%d.Mul(%d)=%d，期望 %d", c.a.Raw(), c.b.Raw(), got.Raw(), c.want.Raw())
		}
	}
	// 有小数部分时 floor 与向零截断在负积上不同：
	// v = -0.5 - 1/2^16（原始 -32769），k = 0.5（原始 32768）。
	// 精确积 = -0.25000762939453125，floor 得 -16385，向零截断得 -16384。
	v, k := FromRaw(-32769), FromRaw(32768)
	if got, want := v.Mul(k), FromRaw(-16385); got != want {
		t.Fatalf("负积取整方向不是 floor: got=%d want=%d", got.Raw(), want.Raw())
	}
}

// TestMulNotAssociativeThreeGroups 是三组“同一物理量、两种相乘顺序”的对照表。
//
// 三栏：差值（规范顺序 - 调换顺序）、界（Mul3Bound）、顺序约定（规范写法）。
// 每组都验证 0 < |差值| <= 界；其中两组界取到等号，说明界是紧的。
func TestMulNotAssociativeThreeGroups(t *testing.T) {
	// dt 与 sim.Integrate 一致：One.Div(FromInt(sub))。
	dtOf := func(sub int) Value { return One.Div(FromInt(sub)) }

	groups := []struct {
		name        string
		sub         int
		v, k        Value
		wantCanon   Value
		wantSwapped Value
		wantBound   Value
	}{
		{
			// v=3.3, k=0.13, 子步 3（dt=21845）
			name: "G1 v=3.3 k=0.13 sub=3",
			sub:  3, v: FromRaw(216268), k: FromRaw(8520),
			wantCanon: FromRaw(9368), wantSwapped: FromRaw(9371), wantBound: FromRaw(4),
		},
		{
			// v=5.3, k=0.13, 子步 5（dt=13107）；差值恰好等于界。
			name: "G2 v=5.3 k=0.13 sub=5",
			sub:  5, v: FromRaw(347341), k: FromRaw(8520),
			wantCanon: FromRaw(9025), wantSwapped: FromRaw(9031), wantBound: FromRaw(6),
		},
		{
			// v=-2.3, k=0.37, 子步 7（dt=9362）；负积且差值恰好等于界。
			name: "G3 v=-2.3 k=0.37 sub=7",
			sub:  7, v: FromRaw(-150733), k: FromRaw(24248),
			wantCanon: FromRaw(-7965), wantSwapped: FromRaw(-7968), wantBound: FromRaw(3),
		},
	}

	for _, g := range groups {
		dt := dtOf(g.sub)
		canon := Mul3(g.v, g.k, dt)
		swapped := g.v.Mul(g.k).Mul(dt) // 故意调换顺序的反例写法
		bound := Mul3Bound(g.v, dt)
		diff := canon - swapped

		if canon != g.wantCanon || swapped != g.wantSwapped {
			t.Errorf("%s: canon=%d swapped=%d，期望 %d/%d",
				g.name, canon.Raw(), swapped.Raw(), g.wantCanon.Raw(), g.wantSwapped.Raw())
		}
		if bound != g.wantBound {
			t.Errorf("%s: 界=%d，期望 %d", g.name, bound.Raw(), g.wantBound.Raw())
		}
		if diff == 0 {
			t.Errorf("%s: 两种顺序结果相同，无法证明不结合", g.name)
		}
		if abs32(diff.Raw()) > bound.Raw() {
			t.Errorf("%s: |差值|=%d 超过界 %d", g.name, abs32(diff.Raw()), bound.Raw())
		}
	}
}

// TestMul3BoundHolds 在可复现的网格与伪随机样本上核对差界定理。
// 这里遍历正负值的组合，确保 floor 语义下界依然成立。
func TestMul3BoundHolds(t *testing.T) {
	grid := []int32{
		-655360, -347341, -150733, -65536, -32769, -1, 0, 1, 4588, 8520,
		13107, 16384, 21845, 32768, 65536, 150733, 216268, 347341, 589824, 655360,
	}
	for _, vr := range grid {
		for _, kr := range grid {
			for _, dr := range grid {
				v, k, dt := FromRaw(int(vr)), FromRaw(int(kr)), FromRaw(int(dr))
				canon := Mul3(v, k, dt)
				swapped := v.Mul(k).Mul(dt)
				diff := abs32(canon.Raw() - swapped.Raw())
				if b := Mul3Bound(v, dt).Raw(); diff > b {
					t.Fatalf("v=%d k=%d dt=%d: |差|=%d > 界 %d", vr, kr, dr, diff, b)
				}
			}
		}
	}

	// 确定性伪随机补充（线性同余，避免依赖 math/rand 版本）。
	seed := int64(12345)
	next := func() int64 {
		seed = (seed*1103515245 + 12345) & 0x7fffffff
		return seed
	}
	for i := 0; i < 20000; i++ {
		v := FromRaw(int(next()%4000000) - 2000000)
		k := FromRaw(int(next()%4000000) - 2000000)
		dt := FromRaw(int(next()%4000000) - 2000000)
		diff := abs32(Mul3(v, k, dt).Raw() - v.Mul(k).Mul(dt).Raw())
		if b := Mul3Bound(v, dt).Raw(); diff > b {
			t.Fatalf("随机样本 v=%d k=%d dt=%d: |差|=%d > 界 %d",
				v.Raw(), k.Raw(), dt.Raw(), diff, b)
		}
	}
}

// TestMul3CoefAndByEquivalent 确认“循环外提系数”的写法与逐个 Mul3 逐位相同，
// 这是顺序约定允许的等价形式。
func TestMul3CoefAndByEquivalent(t *testing.T) {
	k, dt := FromRaw(8520), One.Div(FromInt(3))
	coef := Mul3Coef(k, dt)
	for _, vr := range []int{216268, -150733, 347341, 0, 1 << 20} {
		v := FromRaw(vr)
		if got, want := Mul3By(v, coef), Mul3(v, k, dt); got != want {
			t.Fatalf("v=%d: 外提=%d 逐个=%d", vr, got.Raw(), want.Raw())
		}
	}
}

func abs32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
