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
//
// 注意：除数经过零时输出在零处不连续，且小除数产生的大商会截断为 int32，
// 新代码应优先使用 SatDiv。保留 Div 仅为兼容既有确定性轨迹。
func (v Value) Div(o Value) Value {
	if o == 0 {
		return 0
	}
	return Value((int64(v) << 16) / int64(o))
}

// SatDiv 是带符号的饱和除法，结果向零取整，并夹在 [-sat, +sat]。
// sat 必须为正数，由调用方按物理包络给出（见 docs/division-saturation.md）。
//
// 符号处理约定：
//   - 普通除法在 int64 域完成，商的符号由被除数与除数异号决定（Go 向零取整），
//     最后才收窄为 int32，避免先截断导致符号翻转后再夹到错误的一侧；
//   - 除数为零时不返回零（零会在过零处制造不连续），而是取被除数的符号：
//     被除数非负返回 +sat，为负返回 -sat，被除数本身为零返回 0。
func (v Value) SatDiv(o Value, sat Value) Value {
	if sat <= 0 {
		return 0
	}
	var q int64
	switch {
	case o == 0:
		if v == 0 {
			return 0
		}
		if v > 0 {
			return sat
		}
		return -sat
	case v == 0:
		return 0
	default:
		q = (int64(v) << 16) / int64(o)
	}
	s := int64(sat)
	if q > s {
		return sat
	}
	if q < -s {
		return -sat
	}
	return Value(q)
}
