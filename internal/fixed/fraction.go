package fixed

// FromFraction 由精确有理数 num/den 构造 Q16.16 定点数，按最近值舍入
// （半值向上）。den 为零返回零；支持负数。
//
// 例：FromFraction(49, 5) 即 9.8，原始分子为 642253，
// 表示 642253/65536 = 9.8000030517578125，与真值之差为 +1/327680。
func FromFraction(num, den int64) Value {
	if den == 0 {
		return 0
	}
	neg := (num < 0) != (den < 0)
	if den < 0 {
		den = -den
	}
	if num < 0 {
		num = -num
	}
	// 调用方传入的分子足够小，左移 16 位不会溢出；常量场景均满足。
	scaled := num << 16
	q := scaled / den
	if r := scaled % den; 2*r >= den {
		q++
	}
	if neg {
		q = -q
	}
	return Value(q)
}
