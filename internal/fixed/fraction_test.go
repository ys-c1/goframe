package fixed

import "testing"

func TestFromFraction98(t *testing.T) {
	// 9.8 = 49/5 -> Q16.16 最近分子 642253。
	if got := FromFraction(49, 5).Raw(); got != 642253 {
		t.Fatalf("FromFraction(49,5).Raw()=%d 期望 642253", got)
	}
	if got := FromFraction(98, 10).Raw(); got != 642253 {
		t.Fatalf("FromFraction(98,10).Raw()=%d 期望 642253", got)
	}
	// 误差恰为 +1 raw 对应的 +1/327680。
	want := FromRaw(642253)
	if want.Raw() != 642253 {
		t.Fatal("sanity")
	}
	// 整数路径保持不变。
	if got := FromInt(2).Raw(); got != 2<<16 {
		t.Fatalf("FromInt(2)=%d", got)
	}
}

func TestFromFractionSignAndZeroDen(t *testing.T) {
	if got := FromFraction(-49, 5).Raw(); got != -642253 {
		t.Fatalf("负分数 raw=%d 期望 -642253", got)
	}
	if got := FromFraction(49, -5).Raw(); got != -642253 {
		t.Fatalf("负分母 raw=%d 期望 -642253", got)
	}
	if got := FromFraction(49, 0); got != 0 {
		t.Fatalf("零分母应返回 0，实际 %d", got.Raw())
	}
}
