// Package bucket 把带（整数）时间戳的网络指令映射到本地确定性帧号。
//
// 所有运算都是整数欧几里得运算，不经过 float，因而不存在求和顺序或
// ULP 导致的二义性。唯一性的数学依据见 DivMod 与 docs/bucketing.md。
package bucket

import "errors"

// Mode 是时间戳到帧号的取整规则。
type Mode int

const (
	// Floor 向下取整（欧几里得分桶），修复后的唯一规则。
	Floor Mode = iota
	// RoundHalfUp 四舍五入，半数边界向上（t 恰在半帧点时进后一帧）。
	RoundHalfUp
	// RoundHalfDown 四舍五入，半数边界向下。专门用来暴露平局不唯一。
	RoundHalfDown
	// RoundHalfEven 银行家舍入，半数边界取偶数帧。
	RoundHalfEven
)

// ErrFrameLength 表示帧长不是正整数。
var ErrFrameLength = errors.New("bucket: 帧长必须为正整数")

// Valid 报告帧长是否合法。
func Valid(F int64) bool { return F > 0 }

func check(F int64) {
	if F <= 0 {
		panic(ErrFrameLength)
	}
}

// DivMod 返回欧几里得商 q 与非负余数 r：
//
//	t = q*F + r,  0 <= r < F,  F > 0
//
// 这一对 (q,r) 对每个 t 都是唯一的（整数除法算法）。
// 注意 Go 原生 / 对负数是“向零取整”，余数可能为负，这里修正成向下取整。
func DivMod(t, F int64) (q, r int64) {
	check(F)
	q = t / F
	r = t % F
	if r < 0 {
		q--
		r += F
	}
	return q, r
}

// FloorIndex 返回向下取整帧号 floor(t/F)。
func FloorIndex(t, F int64) int64 {
	q, _ := DivMod(t, F)
	return q
}

// Frame 按指定规则把时间戳 t 分到帧。F 必须为正，否则 panic。
func Frame(t, F int64, m Mode) int64 {
	q, r := DivMod(t, F) // 0 <= r < F
	switch m {
	case Floor:
		return q
	case RoundHalfUp: // 2r >= F 即“达到或超过半帧”，平局向上
		if 2*r >= F {
			q++
		}
		return q
	case RoundHalfDown: // 严格超过半帧才向上，平局留在前一帧
		if 2*r > F {
			q++
		}
		return q
	case RoundHalfEven:
		switch {
		case 2*r > F:
			q++
		case 2*r == F && q&1 != 0: // 平局且 q 为奇数时进到偶数 q+1
			q++
		}
		return q
	default:
		panic("bucket: 未知取整模式")
	}
}

// RoundBoundary2 返回第 k 条四舍五入分界点 c_k 的两倍：
//
//	c_k = (k + 1/2)F,  2*c_k = (2k+1)F
//
// 分界点 c_k 是帧 k 与帧 k+1 的舍入边界。用“两倍”表示可避免
// F 为奇数时出现半整数。
func RoundBoundary2(k, F int64) int64 {
	check(F)
	return (2*k + 1) * F
}

// DistToRoundBoundary2 返回 2*|t - c_k|，即以半时间单位计的距离：
//
//	|2t - (2k+1)F|
//
// 判定 t 是否落在扰动半径 e 内时用 DistToRoundBoundary2 <= 2e。
func DistToRoundBoundary2(t, k, F int64) int64 {
	d := 2*t - RoundBoundary2(k, F)
	if d < 0 {
		d = -d
	}
	return d
}

// IsIntegralTie 报告 c_k 是否为整数时间戳可达的精确平局点。
// c_k = (2k+1)F/2 为整数，当且仅当 (2k+1)F 为偶数；
// 因 2k+1 恒为奇数，这等价于 F 为偶数。
func IsIntegralTie(k, F int64) bool {
	check(F)
	return RoundBoundary2(k, F)%2 == 0
}

// InAmbiguityBand 报告时间戳 t 是否落在第 k 条舍入边界的扰动模糊带内：
//
//	|t - c_k| <= e
//
// e 是同一“逻辑时间戳”在两次求值/两条链路上允许的最大偏差
// （时钟量化、浮点重算、重排求和等）。带宽（连续口径）为 2e。
func InAmbiguityBand(t, k, F, e int64) bool {
	if e < 0 {
		e = -e
	}
	return DistToRoundBoundary2(t, k, F) <= 2*e
}
