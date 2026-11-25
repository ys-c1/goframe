package clock

import (
	"math"
	"testing"
)

// 题目要求的三档帧率与两档帧数。
var (
	rates       = []int{60, 30, 120}
	frameCounts = []int{60, 300}
)

// TestNextMatchesFrameAt 证明分配可复算：状态机逐帧结果与纯函数 FrameAt 逐帧相等。
func TestNextMatchesFrameAt(t *testing.T) {
	for _, r := range rates {
		s, err := New(r)
		if err != nil {
			t.Fatalf("New(%d): %v", r, err)
		}
		for i := 0; i < 1000; i++ {
			got := s.Next()
			want, err := FrameAt(r, i)
			if err != nil {
				t.Fatalf("FrameAt(%d,%d): %v", r, i, err)
			}
			if got != want {
				t.Fatalf("rate=%d idx=%d Next=%d FrameAt=%d", r, i, got, want)
			}
		}
	}
}

// TestOnlyLongOrShort 每帧帧长只可能是 q 或 q+1。
func TestOnlyLongOrShort(t *testing.T) {
	for _, r := range rates {
		s, _ := New(r)
		for i := 0; i < 1000; i++ {
			ms := s.Next()
			if ms != s.ShortMS() && ms != s.LongMS() {
				t.Fatalf("rate=%d idx=%d 非法帧长 %d", r, i, ms)
			}
		}
	}
}

// TestDriftWithinOneFrame 偏差界：每个前缀末尾 |偏差| < 一帧（短帧长），
// 同时验证精确分数与逐帧整数累加得到的实际偏差一致。
func TestDriftWithinOneFrame(t *testing.T) {
	for _, r := range rates {
		s, _ := New(r)
		for _, n := range frameCounts {
			var scheduled int
			for i := 0; i < n; i++ {
				scheduled += s.Next()
				num, den, err := DriftMS(r, i+1)
				if err != nil {
					t.Fatalf("DriftMS: %v", err)
				}
				// 整数对照：scheduled − 1000(i+1)/r，通分后分子为
				// scheduled·r − 1000(i+1)，分母为 r。
				wantNum := scheduled*r - msPerSecond*(i+1)
				wantDen := 1
				if wantNum != 0 {
					g := gcd(abs(wantNum), r)
					wantNum, wantDen = wantNum/g, r/g
				}
				if num != wantNum || den != wantDen {
					t.Fatalf("rate=%d n=%d DriftMS=%d/%d 实际=%d/%d",
						r, i+1, num, den, wantNum, wantDen)
				}
				if math.Abs(float64(num)/float64(den)) >= float64(s.ShortMS()) {
					t.Fatalf("rate=%d n=%d 偏差 %d/%d 不小于一帧 %dms",
						r, i+1, num, den, s.ShortMS())
				}
			}
		}
	}
}

// TestMaxAbsDrift 三档帧率在 60/300 帧内的最大绝对偏差均为 2/3 毫秒（< 1ms，远小于一帧）。
func TestMaxAbsDrift(t *testing.T) {
	want := map[int][2]int{} // rate -> {num, den}
	for _, r := range rates {
		want[r] = [2]int{2, 3}
	}
	for _, r := range rates {
		for _, n := range frameCounts {
			num, den, err := MaxAbsDriftMS(r, n)
			if err != nil {
				t.Fatalf("MaxAbsDriftMS: %v", err)
			}
			w := want[r]
			if num != w[0] || den != w[1] {
				t.Fatalf("rate=%d n=%d max=%d/%d want=%d/%d",
					r, n, num, den, w[0], w[1])
			}
		}
	}
}

// TestEndpointDrift 整周期（n 为周期长度整数倍）末尾偏差必须归零。
func TestEndpointDrift(t *testing.T) {
	for _, r := range rates {
		cycle, err := CycleLen(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []int{cycle, 2 * cycle, 3 * cycle} {
			num, den, err := DriftMS(r, n)
			if err != nil {
				t.Fatal(err)
			}
			if num != 0 || den != 1 {
				t.Fatalf("rate=%d n=%d 周期末尾偏差=%d/%d，应为 0", r, n, num, den)
			}
		}
		// 60 与 300 都是 d=3 的倍数：两档帧数端点偏差均为 0。
		for _, n := range frameCounts {
			num, _, _ := DriftMS(r, n)
			if n%cycle != 0 {
				continue
			}
			if num != 0 {
				t.Fatalf("rate=%d n=%d 端点偏差=%d，应为 0", r, n, num)
			}
		}
	}
}

// TestDistribution 三档帧率的长短帧分布与 2:1 / 1:2 比例，并与逐帧计数一致。
func TestDistribution(t *testing.T) {
	// rate -> 每周期 (long, short)
	wantRatio := map[int][2]int{
		60:  {2, 1}, // 长:短 = 2:1
		30:  {1, 2}, // 长:短 = 1:2
		120: {1, 2}, // 长:短 = 1:2
	}
	// rate -> n 帧期望 (long, short)
	cases := []struct {
		rate, n, long, short int
	}{
		{60, 60, 40, 20},
		{60, 300, 200, 100},
		{30, 60, 20, 40},
		{30, 300, 100, 200},
		{120, 60, 20, 40},
		{120, 300, 100, 200},
	}
	for _, c := range cases {
		long, short, err := Distribution(c.rate, c.n)
		if err != nil {
			t.Fatal(err)
		}
		if long != c.long || short != c.short {
			t.Fatalf("rate=%d n=%d 分布=(%d,%d) want=(%d,%d)",
				c.rate, c.n, long, short, c.long, c.short)
		}
		lp, sp, err := LongShortRatio(c.rate)
		if err != nil {
			t.Fatal(err)
		}
		wr := wantRatio[c.rate]
		if lp != wr[0] || sp != wr[1] {
			t.Fatalf("rate=%d 比例=%d:%d want=%d:%d",
				c.rate, lp, sp, wr[0], wr[1])
		}
		// 与状态机实际计数对拍。
		s, _ := New(c.rate)
		gotLong := 0
		for i := 0; i < c.n; i++ {
			if s.Next() == s.LongMS() && s.LongMS() != s.ShortMS() {
				gotLong++
			}
		}
		if gotLong != long {
			t.Fatalf("rate=%d n=%d 实际长帧=%d 公式=%d", c.rate, c.n, gotLong, long)
		}
	}
}

// TestFixedDriftLinear 朴素固定帧长方案：偏差随帧数线性累积，公式与逐项累加一致。
func TestFixedDriftLinear(t *testing.T) {
	cases := []struct {
		rate, ms, n      int
		wantNum, wantDen int
	}{
		{60, 16, 60, -40, 1},     // 60·(16−50/3) = −40ms
		{60, 16, 300, -200, 1},   // −200ms
		{60, 16, 3600, -2400, 1}, // 一分钟 3600 帧，−2400ms
		{30, 33, 1800, -600, 1},  // 一分钟 1800 帧，−600ms
		{120, 8, 7200, -2400, 1}, // 一分钟 7200 帧，−2400ms
		{60, 17, 3600, 1200, 1},  // 调大帧长：+1200ms，方向反转仍线性
		{60, 16, 0, 0, 1},
	}
	for _, c := range cases {
		num, den, err := FixedDriftMS(c.rate, c.ms, c.n)
		if err != nil {
			t.Fatal(err)
		}
		if num != c.wantNum || den != c.wantDen {
			t.Fatalf("FixedDrift(%d,%d,%d)=%d/%d want=%d/%d",
				c.rate, c.ms, c.n, num, den, c.wantNum, c.wantDen)
		}
	}

	// 线性性：D(2n) = 2·D(n)。
	num1, den1, _ := FixedDriftMS(60, 16, 150)
	num2, den2, _ := FixedDriftMS(60, 16, 300)
	if num2*den1 != 2*num1*den2 {
		t.Fatalf("固定帧长不满足线性: %d/%d vs %d/%d", num2, den2, num1, den1)
	}
}

// TestBresenhamCycle 一个周期 d 帧内，余数寄存器遍历且只遍历到给定余数集合，
// 周期末回到 0，保证分配模式可无限重复、可复算。
func TestBresenhamCycle(t *testing.T) {
	for _, r := range rates {
		s, _ := New(r)
		cycle, _ := CycleLen(r)
		seen := map[int]bool{}
		for i := 0; i < cycle; i++ {
			s.Next()
			if i < cycle-1 {
				seen[s.Remainder()] = true
			}
		}
		if s.Remainder() != 0 {
			t.Fatalf("rate=%d 周期末余数=%d，应为 0", r, s.Remainder())
		}
		// 再跑一个周期，FrameAt 序列必须与第一周期完全相同。
		first := make([]int, cycle)
		second := make([]int, cycle)
		for i := 0; i < cycle; i++ {
			first[i], _ = FrameAt(r, i)
			second[i], _ = FrameAt(r, i+cycle)
			if first[i] != second[i] {
				t.Fatalf("rate=%d 周期不可重复: idx=%d %d vs %d",
					r, i, first[i], second[i])
			}
		}
	}
}

// TestUniformRate 整除帧率（如 100Hz -> 10ms）只有等长帧，任何帧数偏差为 0。
func TestUniformRate(t *testing.T) {
	s, err := New(100)
	if err != nil {
		t.Fatal(err)
	}
	if s.LongMS() != s.ShortMS() || s.ShortMS() != 10 {
		t.Fatalf("100Hz 帧长异常: short=%d long=%d", s.ShortMS(), s.LongMS())
	}
	for _, n := range []int{1, 60, 300, 1000} {
		if ms, _ := FrameAt(100, n-1); ms != 10 {
			t.Fatalf("100Hz 第 %d 帧=%d，应为 10", n, ms)
		}
		num, _, _ := DriftMS(100, n)
		if num != 0 {
			t.Fatalf("100Hz n=%d 偏差=%d，应为 0", n, num)
		}
	}
	lp, sp, err := LongShortRatio(100)
	if err != nil || lp != 0 || sp != 1 {
		t.Fatalf("100Hz 比例=(%d,%d)，应为 (0,1): %v", lp, sp, err)
	}
}

// TestValidation 入参校验。
func TestValidation(t *testing.T) {
	for _, r := range []int{0, -1, 1001} {
		if _, err := New(r); err == nil {
			t.Fatalf("New(%d) 未报错", r)
		}
		if _, err := FrameAt(r, 0); err == nil {
			t.Fatalf("FrameAt(%d,0) 未报错", r)
		}
	}
	if _, err := FrameAt(60, -1); err == nil {
		t.Fatal("负帧号未报错")
	}
	if _, _, err := Distribution(60, -1); err == nil {
		t.Fatal("负帧数未报错")
	}
	if _, _, err := FixedDriftMS(60, -1, 1); err == nil {
		t.Fatal("负固定帧长未报错")
	}
}
