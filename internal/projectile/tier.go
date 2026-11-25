// Package projectile 复现“重力 9.8 定点近似 → 误差按帧数平方累积”的实验。
//
// 设计要点：
//   - 参考解用精确有理数 g*=49/5 走与内核完全相同的半隐式欧拉递推；
//   - 三档定点常量记为 GravRaw/2^FracBits，递推全程只有整数加法，
//     因此重力量化误差 δg 是每帧恒定的加速度差，不引入额外乘法舍入；
//   - 所有“判定/越界帧”用 math/big.Rat 精确计算，float64 仅用于展示与 JSON。
package projectile

import (
	"math"
	"math/big"
	"strconv"
)

// 精确重力 9.8 = 49/5（世界单位/帧²）。
const (
	GravityNum = int64(49)
	GravityDen = int64(5)
)

// Tier 是一档“分子位数”：重力常量被量化为 Numerator()/2^Bits。
type Tier struct {
	Name string // 展示名，如 Q16.16
	Bits int    // 二进制定点小数位数 f
}

// DefaultTiers 是题目要求的三档分子位数：
// Q8.8（分子 2509，4 位）、Q16.16（分子 642253，6 位，仓库现行格式）、
// Q32.32（分子 42090679501，11 位，升精度档）。
func DefaultTiers() []Tier {
	return []Tier{
		{Name: "Q8.8", Bits: 8},
		{Name: "Q16.16", Bits: 16},
		{Name: "Q32.32", Bits: 32},
	}
}

// Scale 返回分母 2^f。
func (t Tier) Scale() int64 { return int64(1) << t.Bits }

// Numerator 返回 round((49/5)·2^f)，即该档常量的整数分子（原始表示）。
func (t Tier) Numerator() int64 {
	s := GravityNum * t.Scale()
	q, r := s/GravityDen, s%GravityDen
	if 2*r >= GravityDen { // 最近舍入，半值向上
		q++
	}
	return q
}

// Digits 返回分子的十进制位数，即“分子位数”。
func (t Tier) Digits() int { return len(strconv.FormatInt(t.Numerator(), 10)) }

// Gravity 返回该档近似重力的浮点值（仅用于展示）。
func (t Tier) Gravity() float64 {
	v, _ := big.NewRat(t.Numerator(), t.Scale()).Float64()
	return v
}

// DGravity 返回精确的每帧重力差 δg = g_tier − 49/5。
// 对 f=8/16/32（均为 4 的倍数）恰好为 +1/(5·2^f)。
func (t Tier) DGravity() *big.Rat {
	return big.NewRat(5*t.Numerator()-GravityNum*t.Scale(), GravityDen*t.Scale())
}

// Coeff 返回位移误差关于帧多项式的二次系数 c = δg/2（带符号）。
// 半隐式欧拉下 Δy_n = −c·n(n+1)，其 n² 项系数绝对值即 |δg|/2。
func (t Tier) Coeff() *big.Rat {
	return new(big.Rat).Mul(t.DGravity(), big.NewRat(1, 2))
}

// AbsCoeff 返回 |δg|/2。
func (t Tier) AbsCoeff() *big.Rat { return absRat(t.Coeff()) }

// ErrAtFrame 返回第 n 帧同帧位置误差的绝对值
// |Δy_n| = |δg|·n(n+1)/2 = c_abs·n(n+1)（精确有理数，世界单位）。
func (t Tier) ErrAtFrame(n int64) *big.Rat {
	nn := new(big.Int).Mul(big.NewInt(n), big.NewInt(n+1))
	return new(big.Rat).Mul(t.AbsCoeff(), new(big.Rat).SetInt(nn))
}

// BreachFrame 返回首个“误差严格大于容差 eps”的帧 k：
// k = min{ n≥1 : ErrAtFrame(n) > eps }。
// eps 为 nil 时使用默认容差 1/10。
func (t Tier) BreachFrame(eps *big.Rat) int64 {
	if eps == nil {
		eps = DefaultTolerance()
	}
	exceeds := func(n int64) bool { return t.ErrAtFrame(n).Cmp(eps) > 0 }
	hi := int64(1)
	for !exceeds(hi) {
		hi *= 2
	}
	lo := int64(0) // 恒不越界（第 0 帧误差为 0）
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		if exceeds(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// DefaultTolerance 是本实验声明的落点容差 ε=0.1 世界单位。
// 题面未给数值，这里取 0.1；命令行可用 --tolerance 覆盖，结论形式不变。
func DefaultTolerance() *big.Rat { return big.NewRat(1, 10) }

// FrameBudgetContinuous 给出连续近似下的帧限 N≈sqrt(2ε/|δg|)（仅参考）。
func (t Tier) FrameBudgetContinuous(eps *big.Rat) float64 {
	if eps == nil {
		eps = DefaultTolerance()
	}
	e, _ := eps.Float64()
	dg, _ := absRat(t.DGravity()).Float64()
	return math.Sqrt(2 * e / dg)
}

func absRat(r *big.Rat) *big.Rat {
	if r.Sign() < 0 {
		return new(big.Rat).Neg(r)
	}
	return new(big.Rat).Set(r)
}
