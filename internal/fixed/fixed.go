// Package fixed 提供 Q16.16 定点数，避免浮点在不同机器上的差异。
package fixed

// Value 是 Q16.16 定点数。
type Value int32

const One = Value(1 << 16)

// FromInt 由整数构造。
func FromInt(v int) Value { return Value(v) << 16 }

// FromRaw 由原始表示构造。
func FromRaw(v int) Value { return Value(v) }

// Raw 返回原始表示。
func (v Value) Raw() int32 { return int32(v) }

// Add 定点加法。
func (v Value) Add(o Value) Value { return v + o }

// Mul 定点乘法，右移十六位，向零取整。
func (v Value) Mul(o Value) Value {
	return Value((int64(v) * int64(o)) >> 16)
}

// Div 定点除法，被零除返回零，结果向零取整。
func (v Value) Div(o Value) Value {
	if o == 0 {
		return 0
	}
	return Value((int64(v) << 16) / int64(o))
}

// MaxFracBits 是 Q16.16 能保留的最大二进制小数位。
const MaxFracBits = 16

// GridStep 返回保留 fracBits 个定点小数位时的原始量化步长：
// fracBits 位小数对应真实步长 2^-fracBits，换算成 raw 即 2^(16-fracBits)。
// fracBits 超出 [0,16] 会被夹到区间内。
func GridStep(fracBits int) Value {
	if fracBits < 0 {
		fracBits = 0
	}
	if fracBits > MaxFracBits {
		fracBits = MaxFracBits
	}
	return Value(1) << (MaxFracBits - fracBits)
}

// QuantizeToStep 把 v 舍入到正整数网格步长 step（raw 单位）的最近整数倍，
// 半格远离零取整（tie away from zero），全平台结果一致。
// step<=1 时每个 raw 整数本身就是网格点，原样返回。
func QuantizeToStep(v Value, step Value) Value {
	q := int64(step)
	if q <= 1 {
		return v
	}
	x := int64(v)
	neg := x < 0
	a := x
	if neg {
		a = -a
	}
	// floor((2a+q)/(2q))：r>=q/2 时进一格，恰好半格也进一格。
	m := (2*a + q) / (2 * q)
	r := m * q
	if neg {
		r = -r
	}
	if r > 1<<31-1 {
		r = 1<<31 - 1
	}
	if r < -(1 << 31) {
		r = -(1 << 31)
	}
	return Value(r)
}

// QuantizeToFracBits 按定点小数位量化，等价于 QuantizeToStep(v, GridStep(bits))。
func QuantizeToFracBits(v Value, bits int) Value {
	return QuantizeToStep(v, GridStep(bits))
}
