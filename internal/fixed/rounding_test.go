package fixed

import "testing"

// TestMulRoundingSemantics 用一个无法被 Q16 精确表示的最小负乘积证明：
// 算术右移是向下取整，而整数除法才是向零取整。
// 原始乘积 p = raw(a)*raw(b) = -1，精确商为 -1/65536。
//   floor(-1/65536) = -1
//   trunc(-1/65536) =  0
func TestMulRoundingSemantics(t *testing.T) {
	a := FromRaw(1)
	b := FromRaw(-1)

	if got := a.MulFloor(b).Raw(); got != -1 {
		t.Fatalf("MulFloor(1,-1) = %d, 期望 -1 (floor)", got)
	}
	if got := a.Mul(b).Raw(); got != -1 {
		t.Fatalf("Mul 必须保持历史行为 MulFloor = %d, 期望 -1", got)
	}
	if got := a.MulTrunc(b).Raw(); got != 0 {
		t.Fatalf("MulTrunc(1,-1) = %d, 期望 0 (trunc toward zero)", got)
	}

	// 正数时两种取整一致：floor(1/65536)=trunc(1/65536)=0。
	if got := FromRaw(1).MulFloor(a).Raw(); got != 0 {
		t.Fatalf("正小乘积 floor 应为 0, got %d", got)
	}
	if got := FromRaw(1).MulTrunc(a).Raw(); got != 0 {
		t.Fatalf("正小乘积 trunc 应为 0, got %d", got)
	}

	// 可精确表示的乘积（低 16 位为 0）两种模式都无误差。
	x := FromInt(3)
	if x.MulFloor(FromInt(2)) != FromInt(6) || x.MulTrunc(FromInt(2)) != FromInt(6) {
		t.Fatal("整数乘法应精确得到 6")
	}
}
