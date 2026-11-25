// Package fixed 提供 Q16.16 定点数，避免浮点在不同机器上的差异。
//
// 定点乘法的取整方向与三因子乘积的结合顺序都是本仓库的确定性契约，
// 不得随手改动；推导、数值验证与反例见 docs/mul-associativity.md。
package fixed

// Value 是 Q16.16 定点数。
type Value int32

const (
	// One 是 1.0 的原始表示。
	One = Value(1 << 16)
	// fracBits 是小数位数；取整方向与差界都以 2^fracBits 为准。
	fracBits = 16
)

// FromInt 由整数构造。
func FromInt(v int) Value { return Value(v) << fracBits }

// FromRaw 由原始表示构造。
func FromRaw(v int) Value { return Value(v) }

// Raw 返回原始表示。
func (v Value) Raw() int32 { return int32(v) }

// Add 定点加法。
func (v Value) Add(o Value) Value { return v + o }

// Sub 定点减法。
func (v Value) Sub(o Value) Value { return v - o }

// Mul 定点乘法：64 位精确相乘后算术右移十六位。
//
// 算术右移对负数等价于向下取整（floor），而不是向零取整：
// 例如 Mul(FromRaw(-1), One) 得 -1，Mul(FromRaw(-65537), One) 得 -2。
// 这是欧几里得余数意义下的取整，Mul3Bound 的证明依赖“余数 r 恒满足
// 0 <= r < 2^16”；若改成截断或四舍五入，差界与 docs 里的数值都要重算，
// 状态指纹也会跟着变。
func (v Value) Mul(o Value) Value {
	return Value((int64(v) * int64(o)) >> fracBits)
}

// Div 定点除法，被零除返回零，结果向零取整。
func (v Value) Div(o Value) Value {
	if o == 0 {
		return 0
	}
	return Value((int64(v) << fracBits) / int64(o))
}

// 顺序约定（全仓库唯一，改动会使状态与回放指纹变化）：
//
//	三因子乘积一律写成 v*(k*dt)：先把后两个“系数”因子结合成
//	coef = k*dt，再与状态量 v 相乘。即右结合、系数对先行。
//	禁止写成 (v*k)*dt；也不允许编译器/重构把结合顺序调换。
//
// 固定点乘法不满足结合律，两种顺序最多相差 ceil((|v|+|dt|)/2^16)
// 个原始单位（与 k 无关），足以让锁步对局与回放在数帧后分叉。
// 统一走 Mul3 / Mul3Coef / Mul3By 这三个入口，是让差为零的唯一办法。

// Mul3 计算三因子乘积 v*(k*dt)，是顺序约定的规范写法。
func Mul3(v, k, dt Value) Value { return v.Mul(k.Mul(dt)) }

// Mul3Coef 先算出本步系数 coef = k*dt，供循环外提复用。
// 同一帧内所有实体共用同一个 coef，结果与逐个调用 Mul3 逐位相同。
func Mul3Coef(k, dt Value) Value { return k.Mul(dt) }

// Mul3By 用已算好的系数完成 v*coef，等价于 Mul3(v, k, dt)。
func Mul3By(v, coef Value) Value { return v.Mul(coef) }

// Mul3Bound 返回 Mul3(v,k,dt) 与左结合写法 (v*k)*dt 之差的绝对值上界，
// 单位为原始单位：ceil((|v|+|dt|)/2^16)。
//
// 上界只依赖 v 与 dt，与 k 无关，因此不用等系数定下来就能预估误差。
func Mul3Bound(v, dt Value) Value {
	span := abs64(int64(v)) + abs64(int64(dt))
	return Value((span + (1 << fracBits) - 1) >> fracBits)
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
