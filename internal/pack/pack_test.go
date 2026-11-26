package pack

import "testing"

func TestQuantizeKnown(t *testing.T) {
	cases := []struct {
		x    int32
		bits int
		want int32
	}{
		{99840, 21, 98304},   // 48 * 2048
		{99840, 24, 99840},   // 390 * 256
		{99840, 28, 99840},   // 1560 * 16，步长整除原值
		{-99840, 21, -100352}, // 算术右移向下取整: (-49)<<11
		{-1, 21, -2048},       // (-1 >> 11) << 11
		{212992, 21, 212992}, // 恰为 2048 的倍数
	}
	for _, c := range cases {
		if got := Quantize(c.x, c.bits); got != c.want {
			t.Errorf("Quantize(%d,%d)=%d want %d", c.x, c.bits, got, c.want)
		}
	}
}

func TestErrorBound(t *testing.T) {
	// 全量 int32 抽样验证 |x-Q(x)| <= Δ-1。
	for _, bits := range []int{1, 7, 16, 21, 24, 28, 32} {
		bound := MaxAbsError(bits)
		for x := int64(-1_000_000); x <= 1_000_000; x++ {
			q := int64(Quantize(int32(x), bits))
			d := x - q
			if d < 0 {
				d = -d
			}
			if d > bound {
				t.Fatalf("bits=%d x=%d err=%d 超界 %d", bits, x, d, bound)
			}
		}
	}
}

func TestMinBits(t *testing.T) {
	// 位置上限 65536 -> 16；速度上限 2048 -> 21。
	if got := MinBits(65536); got != 16 {
		t.Fatalf("MinBits(65536)=%d want 16", got)
	}
	if got := MinBits(2048); got != 21 {
		t.Fatalf("MinBits(2048)=%d want 21", got)
	}
	if got := MinBits(10); got != 29 {
		t.Fatalf("MinBits(10)=%d want 29", got)
	}
	// 反解自洽：返回位宽的先验上界不超过给定上限。
	for _, limit := range []int64{1, 9, 10, 100, 2048, 65536} {
		b := MinBits(limit)
		if MaxAbsError(b) > limit {
			t.Fatalf("limit=%d bits=%d 上界 %d 超限", limit, b, MaxAbsError(b))
		}
		if b > 1 && MaxAbsError(b-1) <= limit {
			t.Fatalf("limit=%d 位宽 %d 并非最小（%d 已够）", limit, b, b-1)
		}
	}
}

func TestPackRoundTrip(t *testing.T) {
	frames := [][]int32{
		{99840, 0, 99840, 0, -99840, -1, 2147483647, -2147483648},
		{212992, 113152, 199680, 0, 50368, 81930, 13312, 1},
	}
	for _, bits := range []int{1, 21, 24, 28, 32} {
		data, err := Pack(frames, bits)
		if err != nil {
			t.Fatal(err)
		}
		got, rb, err := Unpack(data)
		if err != nil || rb != bits {
			t.Fatalf("bits=%d unpack: %v rb=%d", bits, err, rb)
		}
		for f := range frames {
			for i, x := range frames[f] {
				if want := Quantize(x, bits); got[f][i] != want {
					t.Fatalf("bits=%d f=%d i=%d got=%d want=%d", bits, f, i, got[f][i], want)
				}
			}
		}
	}
}

func TestB32Lossless(t *testing.T) {
	frames := [][]int32{{2147483647, -2147483648, 0, 1, -1}}
	data, _ := Pack(frames, 32)
	got, _, err := Unpack(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := range frames[0] {
		if got[0][i] != frames[0][i] {
			t.Fatalf("32 位应无损: got=%d want=%d", got[0][i], frames[0][i])
		}
	}
}

func TestRejects(t *testing.T) {
	if _, err := Pack([][]int32{{1}}, 0); err == nil {
		t.Fatal("位宽 0 应报错")
	}
	if _, err := Pack([][]int32{{1}}, 33); err == nil {
		t.Fatal("位宽 33 应报错")
	}
	bad := make([]byte, headerSize)
	copy(bad, "XXXXXXX0")
	if _, _, err := Unpack(bad); err == nil {
		t.Fatal("魔数错误应报错")
	}
}
