// Package fixed 提供 Q16.16 定点数，避免浮点在不同机器上的差异。
package fixed

// Value 是 Q16.16 定点数。
type Value int32

const (
	fracBits = 16
	One      = Value(1 << fracBits)
)

// FromInt 由整数构造。
func FromInt(v int) Value { return Value(v) << 16 }

// FromRaw 由原始表示构造。
func FromRaw(v int) Value { return Value(v) }

// Raw 返回原始表示。
func (v Value) Raw() int32 { return int32(v) }

// Add 定点加法。
func (v Value) Add(o Value) Value { return v + o }

// Mul 是 MulFloor 的保留入口：有符号乘积算术右移十六位。
// 注意：有符号负数的算术右移等价“向下取整(floor, 朝 -∞)”，并不是“向零取整”。
// 需要向零取整请显式调用 MulTrunc。行为与历史版本完全一致。
func (v Value) Mul(o Value) Value {
	return v.MulFloor(o)
}

// MulFloor 定点乘法后向下取整（floor，朝 -∞）。
// Go 对有符号负整数的 >> 是算术右移，数学上等价 floor(p/2^16)。
func (v Value) MulFloor(o Value) Value {
	return Value((int64(v) * int64(o)) >> fracBits)
}

// MulTrunc 定点乘法后向零取整。Go 的有符号整数除法 / 对负商向零截断，
// 因此 p/2^16 即 trunc(p/2^16)。
func (v Value) MulTrunc(o Value) Value {
	return Value((int64(v) * int64(o)) / int64(One))
}

// Div 定点除法，被零除返回零，结果向零取整。
func (v Value) Div(o Value) Value {
	if o == 0 {
		return 0
	}
	return Value((int64(v) << 16) / int64(o))
}
