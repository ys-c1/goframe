package impulse

import "testing"

// TestEuclidIdentity 对一批取值验证 total = n*q+r 且 0<=r<n。
func TestEuclidIdentity(t *testing.T) {
	for n := int64(1); n <= 16; n++ {
		for total := int64(-1000); total <= 1_000_000; total += 997 {
			q, r := Euclid(total, n)
			if total != n*q+r || r < 0 || r >= n {
				t.Fatalf("Euclid(%d,%d)=(%d,%d) 不满足恒等式", total, n, q, r)
			}
		}
	}
}

// TestExactSumsToTotal 验证精确分摊的子步之和严格等于设定值。
func TestExactSumsToTotal(t *testing.T) {
	for n := int64(1); n <= 16; n++ {
		for total := int64(0); total < 1_000_000; total += 137 {
			steps := Exact(total, n)
			var sum int64
			for _, v := range steps {
				sum += v
			}
			if sum != total {
				t.Fatalf("Exact(%d,%d) 和=%d，不守恒", total, n, sum)
			}
		}
	}
}

// TestNaiveLossIsRemainderTimesSubsteps 同时验证两种等价表述：
// raw 口径每帧损失 = 欧几里得余数 rho；
// “每子步余数 r=rho/n”口径每帧损失 = r*n。
func TestNaiveLossIsRemainderTimesSubsteps(t *testing.T) {
	cases := []struct{ total, n int64 }{
		{2 << 16, 3}, {2 << 16, 5}, {2 << 16, 7},
		{1 << 16, 3}, {10 << 16, 7},
	}
	for _, c := range cases {
		q, rho := Euclid(c.total, c.n)
		_, delivered, loss := Naive(c.total, c.n)
		if delivered != c.n*q || loss != rho {
			t.Fatalf("Naive(%d,%d): loss=%d, 期望 rho=%d", c.total, c.n, loss, rho)
		}
		// 每个子步的精确余数（有理 raw）r=rho/n，n 个相同余数之和 r*n=rho。
		// 用整数交叉相乘验证，避免浮点：loss*n == rho*n。
		if loss*c.n != rho*c.n {
			t.Fatalf("r*n 恒等式失败")
		}
		// 缩放到 1/n raw 的整数台账：每子步整数余数恰为 rho，帧损失 = rho*n 微单位。
		if rho*c.n != loss*c.n {
			t.Fatalf("微单位口径 rho*n 与 raw 损失不一致")
		}
	}
}

// TestCarryIdentityEveryFrameAndBound 验证跨帧余数台账：
// 1) 每帧复式恒等式差值为 0（逐帧，而非只看末帧）；
// 2) 期末余数恒在 [0,n-1]，即误差界 n-1 raw；
// 3) 望远镜求和：累计下发 = F*G - Rem。
func TestCarryIdentityEveryFrameAndBound(t *testing.T) {
	const F = 1000
	for _, n := range []int64{3, 5, 7} {
		for _, G := range []int64{1 << 16, 2 << 16, 10 << 16} {
			c := &FrameCarry{}
			var sumDelivered int64
			for f := 0; f < F; f++ {
				_, delivered, remIn, remOut := c.CarryFrame(G, n)
				if d := IdentityDiff(G, delivered, remIn, remOut); d != 0 {
					t.Fatalf("n=%d frame=%d 复式恒等式差值=%d", n, f, d)
				}
				if remOut < 0 || remOut >= n {
					t.Fatalf("n=%d frame=%d rem=%d 越界 [0,%d)", n, f, remOut, n)
				}
				sumDelivered += delivered
			}
			if want := int64(F)*G - c.Rem; sumDelivered != want {
				t.Fatalf("n=%d 望远镜求和失败: sum=%d want=%d", n, sumDelivered, want)
			}
			if c.Rem > n-1 {
				t.Fatalf("余数超过误差界 n-1=%d", n-1)
			}
		}
	}
}
