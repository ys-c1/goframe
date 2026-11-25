package fixed

import "testing"

func TestGridStepPowers(t *testing.T) {
	cases := map[int]Value{
		6:  1 << 10, // 2^(16-6)=1024
		8:  1 << 8,  // 256
		10: 1 << 6,  // 64
		16: 1,
		0:  One,
	}
	for bits, want := range cases {
		if got := GridStep(bits); got != want {
			t.Fatalf("GridStep(%d)=%d, want %d", bits, got, want)
		}
	}
}

func TestQuantizeNearestAndErrorBound(t *testing.T) {
	for _, bits := range []int{6, 8, 10} {
		q := GridStep(bits)
		emax := int64(q) / 2
		// 扫过一整段 raw，误差永远不超过半格，且结果是 q 的整数倍。
		for v := -200000; v <= 200000; v++ {
			got := QuantizeToFracBits(Value(v), bits)
			if int64(got)%int64(q) != 0 {
				t.Fatalf("bits=%d v=%d 量化结果 %d 不是步长 %d 的整数倍", bits, v, got, q)
			}
			e := int64(got) - int64(v)
			if e < 0 {
				e = -e
			}
			if e > emax {
				t.Fatalf("bits=%d v=%d 误差 %d 超过半格 %d", bits, v, e, emax)
			}
		}
	}
}

func TestQuantizeHalfTieAwayFromZero(t *testing.T) {
	// Q=256，半格 128：+128 -> +256，-128 -> -256。
	if got := QuantizeToFracBits(128, 8); got != 256 {
		t.Fatalf("+half tie = %d, want 256", got)
	}
	if got := QuantizeToFracBits(-128, 8); got != -256 {
		t.Fatalf("-half tie = %d, want -256", got)
	}
	if got := QuantizeToFracBits(127, 8); got != 0 {
		t.Fatalf("127 = %d, want 0", got)
	}
	if got := QuantizeToFracBits(384, 8); got != 512 {
		t.Fatalf("384 = %d, want 512", got)
	}
}

func TestQuantizeOddStepMaxError(t *testing.T) {
	// 反解会给出奇数整数步长（如 1249），最大误差应为 floor(Q/2)=624。
	const q = Value(1249)
	for v := -100000; v <= 100000; v++ {
		got := QuantizeToStep(Value(v), q)
		e := int64(got) - int64(v)
		if e < 0 {
			e = -e
		}
		if e > 624 {
			t.Fatalf("odd step v=%d error=%d > 624", v, e)
		}
	}
	// 624 距 0 为 624、距 1249 为 625，应舍到 0，误差恰为 624。
	if got := QuantizeToStep(624, q); got != 0 {
		t.Fatalf("624 -> %d, want 0", got)
	}
}
