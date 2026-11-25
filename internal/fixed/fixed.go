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
