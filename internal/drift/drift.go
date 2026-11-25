// Package drift 给出指令量化误差经积分累积后的漂移函数、最坏情况上界，
// 以及在给定漂移上限时反解最大量化步长的公式。
//
// 本内核每帧执行 FrameLen=SubSteps 次 “pos += vel” 积分（每子步积分系数为 1），
// 因此帧长 dt 以积分子步为单位时就是 FrameLen；若子步物理时长为 h，
// 则一般物理式中 dt 应替换为 FrameLen*h。
package drift

// WeightSum 返回累积权重和：
//
//	W = dt * N(N+1)/2
//
// 第 t 帧（t=0..N-1）注入的速度误差 e_t，在当帧及之后每帧被积分 dt 次，
// 到第 N 帧位置上的总权重为 dt*(N-t)，求和即得 W。
func WeightSum(frames, frameLen int) int64 {
	n := int64(frames)
	d := int64(frameLen)
	return d * n * (n + 1) / 2
}

// MaxRoundingError 是整数网格步长 step（raw）下四舍五入的最大误差：
// Emax = floor(step/2)。
func MaxRoundingError(stepRaw int64) int64 {
	if stepRaw < 0 {
		stepRaw = -stepRaw
	}
	return stepRaw / 2
}

// BoundRaw 是单条指令/实体/帧、单分量情况下第 N 帧位置漂移的最坏情况上界
// （Q16.16 raw 单位）：
//
//	D_bound = floor(Q/2) * dt * N(N+1)/2 = Q*dt*N(N+1)/4（Q 为偶数时）。
//
// 若同一实体同一帧最多有 m 条指令，结果再乘 m。
func BoundRaw(stepRaw int64, frames, frameLen int) int64 {
	return MaxRoundingError(stepRaw) * WeightSum(frames, frameLen)
}

// VelocityBoundRaw 给出速度误差的线性累积上界 Emax*N，
// 用于对照说明位置漂移的二次放大。
func VelocityBoundRaw(stepRaw int64, frames int) int64 {
	return MaxRoundingError(stepRaw) * int64(frames)
}

// MaxIntegerStepRaw 按漂移上限 capRaw 反解允许的“最大整数网格步长” Qmax。
// 要求 floor(Q/2)*W <= cap，令 E=floor(cap/W)，则最大整数 Q = 2E+1。
// 当 cap 小到连 Q=1（不量化）都不满足时返回 0。
func MaxIntegerStepRaw(capRaw int64, frames, frameLen int) int64 {
	w := WeightSum(frames, frameLen)
	if w <= 0 {
		return 0
	}
	e := capRaw / w
	q := 2*e + 1
	if BoundRaw(q, frames, frameLen) > capRaw {
		return 0
	}
	return q
}

// MaxContinuousStepRaw 返回连续公式反解出的最大步长（raw 浮点值）：
//
//	q_max = 4*cap / (dt*N(N+1))
//
// 真实单位下即 q_max_real = 4*Delta / (dt*N(N+1))。
func MaxContinuousStepRaw(capRaw float64, frames, frameLen int) float64 {
	n := float64(frames)
	d := float64(frameLen)
	return 4 * capRaw / (d * n * (n + 1))
}

// MaxPowerOfTwoStepRaw 返回满足漂移上限的最大 2 的幂 raw 步长
// （即 Q=2^(16-bits)），找不到（上限为负等）时返回 1。
func MaxPowerOfTwoStepRaw(capRaw int64, frames, frameLen int) int64 {
	best := int64(1)
	for p := 1; p <= 16; p++ {
		q := int64(1) << p
		if BoundRaw(q, frames, frameLen) <= capRaw {
			best = q
		}
	}
	return best
}

// FracBitsForStepRaw 把 2 的幂 raw 步长换算成定点小数位 k=16-log2(Q)。
// 非 2 的幂时返回 -1。
func FracBitsForStepRaw(stepRaw int64) int {
	if stepRaw <= 0 || stepRaw&(stepRaw-1) != 0 {
		return -1
	}
	p := 0
	for stepRaw > 1 {
		stepRaw >>= 1
		p++
	}
	return 16 - p
}
