package fixed

import "testing"

func TestDivZeroReturnsZero(t *testing.T) {
	// 旧语义：除零返回零，保留该行为以兼容既有轨迹。
	if got := FromInt(6).Div(0); got != 0 {
		t.Fatalf("Div 除零应返回零，得到 %d", got)
	}
}

func TestSatDivMatchesDivInNormalRange(t *testing.T) {
	sat := FromInt(20)
	cases := []struct {
		a, b int
		want Value
	}{
		{6, 2, FromInt(3)},
		{6, -2, FromInt(-3)},
		{-6, 2, FromInt(-3)},
		{-6, -2, FromInt(3)},
		{7, 2, FromInt(3).Add(One / 2)}, // 3.5：向零取整作用在 Q16.16 小数位
		{0, 2, 0},
	}
	for _, c := range cases {
		got := FromInt(c.a).SatDiv(FromInt(c.b), sat)
		if got != c.want {
			t.Fatalf("SatDiv(%d,%d) = %v, 想要 %v", c.a, c.b, got, c.want)
		}
		// 未触发饱和时必须与旧除法逐位一致。
		if got != FromInt(c.a).Div(FromInt(c.b)) {
			t.Fatalf("SatDiv(%d,%d) 在常规区间与 Div 不一致", c.a, c.b)
		}
	}
}

func TestSatDivZeroDivisorKeepsSign(t *testing.T) {
	sat := FromInt(20)
	if got := FromInt(6).SatDiv(0, sat); got != sat {
		t.Fatalf("正被除数除零应饱和到 +sat，得到 %v", got)
	}
	if got := FromInt(-6).SatDiv(0, sat); got != -sat {
		t.Fatalf("负被除数除零应饱和到 -sat，得到 %v", got)
	}
	if got := Value(0).SatDiv(0, sat); got != 0 {
		t.Fatalf("零除以零应为零，得到 %v", got)
	}
}

func TestSatDivClampsBothSigns(t *testing.T) {
	sat := FromInt(20)
	// 商超过 ±sat 时夹紧，符号随被除数与除数的异号关系。
	if got := FromInt(40).SatDiv(FromInt(1), sat); got != sat {
		t.Fatalf("正商应夹到 +sat，得到 %v", got)
	}
	if got := FromInt(-40).SatDiv(FromInt(1), sat); got != -sat {
		t.Fatalf("负商应夹到 -sat，得到 %v", got)
	}
	if got := FromInt(40).SatDiv(FromInt(-1), sat); got != -sat {
		t.Fatalf("异号应夹到 -sat，得到 %v", got)
	}
	// 恰好等于边界不夹。
	if got := FromInt(20).SatDiv(FromInt(1), sat); got != sat {
		t.Fatalf("商等于 sat 时应原样返回，得到 %v", got)
	}
	// 非法阈值（非正）退化为零，防止调用方配置错误放大输出。
	if got := FromInt(6).SatDiv(FromInt(2), 0); got != 0 {
		t.Fatalf("sat 非正时应返回零，得到 %v", got)
	}
}

func TestSatDivSurvivesInt32Truncation(t *testing.T) {
	// 2^30 除以原始值 1：真实商为 2^46，旧除法截断为 int32 后归零，
	// 符号与量级同时损坏；饱和除法必须在 int64 域夹紧并保留符号。
	sat := FromInt(20)
	big := FromRaw(1 << 30)
	if got := big.Div(FromRaw(1)); got != 0 {
		t.Fatalf("前置条件不成立：旧除法未发生截断归零，得到 %d", got)
	}
	if got := big.SatDiv(FromRaw(1), sat); got != sat {
		t.Fatalf("正大量级输入应夹到 +sat，得到 %v", got)
	}
	if got := FromRaw(-(1 << 30)).SatDiv(FromRaw(1), sat); got != -sat {
		t.Fatalf("负大量级输入应夹到 -sat，得到 %v", got)
	}
}
