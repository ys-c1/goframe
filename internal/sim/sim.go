// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import (
	"goframe/internal/fixed"
	"goframe/internal/impulse"
)

// State 是所有实体的位置与速度，按固定顺序存放，便于逐字节比较。
type State struct {
	posX, posY []fixed.Value
	velX, velY []fixed.Value
	gravity    fixed.Value

	// 重力冲量的精确整数台账（Q16.16 raw，int64，不受 int32 状态回绕影响）。
	// 重力对所有实体相同，因此这里记的是“每个实体”收到的重力冲量。
	subPhase         int   // 下一子步在一帧分摊序列中的下标（mod sub）
	lastFrameGravity int64 // 最近一个完整帧内所有子步重力增量之和
	totalGravity     int64 // 自建态以来每实体累计重力冲量
}

// NewState 创建实体数为 n 的状态。
func NewState(n int, gravity fixed.Value) *State {
	return &State{
		posX:    make([]fixed.Value, n),
		posY:    make([]fixed.Value, n),
		velX:    make([]fixed.Value, n),
		velY:    make([]fixed.Value, n),
		gravity: gravity,
	}
}

// Len 返回实体数。
func (s *State) Len() int { return len(s.posX) }

// Push 给实体施加冲量。
func (s *State) Push(i int, dx, dy fixed.Value) {
	s.velX[i] = s.velX[i].Add(dx)
	s.velY[i] = s.velY[i].Add(dy)
}

// GravitySchedule 把一帧的重力 gravity 严格分摊成 sub 个子步增量。
//
// 设 G = gravity.Raw()，欧几里得分解 G = q*sub + r（0 <= r < sub），
// 则返回 sub 个元素：前 sub-r 个为 q，末尾 r 个为 q+1。
// 因此所有元素 raw 之和严格等于 G——整除余数 r 被拆成 r 个 +1 raw 发掉，
// 而不是丢弃。+1 放在末尾可保证第一个子步与旧的整除结果完全一致。
func GravitySchedule(gravityRaw int32, sub int) []fixed.Value {
	if sub <= 0 {
		sub = 1
	}
	rawSteps := impulse.Exact(int64(gravityRaw), int64(sub))
	out := make([]fixed.Value, len(rawSteps))
	for i, v := range rawSteps {
		out[i] = fixed.FromRaw(int(v))
	}
	return out
}

// Integrate 做一次子步积分（保持“一次调用 = 一个子步”的原始语义）。
//
// sub 是每帧总子步数。重力增量取自 GravitySchedule 的第 phase 项，phase 在
// 0..sub-1 间循环。因此像 api.Step 那样每帧连续调用 sub 次，施加的重力冲量
// 之和严格等于 s.gravity（逐帧守恒）；任意连续 sub 次调用也恰好遍历整个分摊
// 序列、总和为 G。+1 补偿点放在序列末尾，故每个帧的第一次 Integrate(sub)
// 所用增量与旧的整除结果 q 完全相同。
//
// 每个子步运算顺序保持为 velY += g; posX += velX; posY += velY
// （半隐式欧拉）。当 G 能被 sub 整除时（例如 sub=1/2/4/8），整个状态与旧实现
// 逐字节相同。
func (s *State) Integrate(sub int) {
	if sub <= 0 {
		sub = 1
	}
	idx := s.subPhase % sub
	s.subPhase = (idx + 1) % sub
	if idx == 0 {
		s.lastFrameGravity = 0 // 新的一帧开始
	}
	g := GravitySchedule(s.gravity.Raw(), sub)[idx]
	s.lastFrameGravity += int64(g.Raw())
	s.totalGravity += int64(g.Raw())
	for i := range s.posX {
		s.velY[i] = s.velY[i].Add(g)
		s.posX[i] = s.posX[i].Add(s.velX[i])
		s.posY[i] = s.posY[i].Add(s.velY[i])
	}
}

// LastFrameGravityImpulse 返回最近一帧每个实体实际收到的重力冲量（raw）。
func (s *State) LastFrameGravityImpulse() int64 { return s.lastFrameGravity }

// TotalGravityImpulse 返回自建态以来每个实体累计收到的重力冲量（raw）。
func (s *State) TotalGravityImpulse() int64 { return s.totalGravity }

// Raw 按固定顺序导出全部数值，便于逐字节比较。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, 4*len(s.posX))
	for i := range s.posX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}
