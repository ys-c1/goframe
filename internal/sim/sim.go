// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import "goframe/internal/fixed"

// State 是所有实体的位置与速度，按固定顺序存放，便于逐字节比较。
type State struct {
	posX, posY []fixed.Value
	velX, velY []fixed.Value
	gravity    fixed.Value
	frameDT    fixed.Value
}

// NewState 创建实体数为 n 的状态。
// 一帧的默认时长为 1（即速度单位为“格/帧”），保持既有调用语义。
func NewState(n int, gravity fixed.Value) *State {
	return NewStateWithDT(n, gravity, fixed.One)
}

// NewStateWithDT 创建状态并显式指定一帧的 Q16.16 时长 dt。
// 子步长为 dt/sub；积分时速度增量与位移都必须乘这个步长。
func NewStateWithDT(n int, gravity, frameDT fixed.Value) *State {
	if frameDT <= 0 {
		frameDT = fixed.One
	}
	return &State{
		posX:    make([]fixed.Value, n),
		posY:    make([]fixed.Value, n),
		velX:    make([]fixed.Value, n),
		velY:    make([]fixed.Value, n),
		gravity: gravity,
		frameDT: frameDT,
	}
}

// Len 返回实体数。
func (s *State) Len() int { return len(s.posX) }

// Push 给实体施加冲量。
func (s *State) Push(i int, dx, dy fixed.Value) {
	s.velX[i] = s.velX[i].Add(dx)
	s.velY[i] = s.velY[i].Add(dy)
}

// SetLaunch 直接设置实体的位置和速度，供弹道实验构造同一初始条件。
func (s *State) SetLaunch(i int, x, y, vx, vy fixed.Value) {
	s.posX[i] = x
	s.posY[i] = y
	s.velX[i] = vx
	s.velY[i] = vy
}

// Position 返回实体位置的 Q16.16 原始值。
func (s *State) Position(i int) (x, y fixed.Value) {
	return s.posX[i], s.posY[i]
}

// Velocity 返回实体速度的 Q16.16 原始值。
func (s *State) Velocity(i int) (vx, vy fixed.Value) {
	return s.velX[i], s.velY[i]
}

// Integrate 做一次常加速度子步的分量精确运动学积分，sub 为每帧总子步数。
//
// 旧实现只把重力除以 sub，却没有把位移乘子步长 1/sub；sub>1 时位置会按
// sub 倍过冲。这里按 x/y 分量分别带入子步长 dt=frameDT/sub：
//
//	pos_c += vel_c*dt + 1/2*a_c*dt^2
//	vel_c += a_c*dt
//
// 对恒定重力而言，该格式消去了一阶 Euler 的 O(dt^2) 局部截断项；剩余落点
// 误差只来自 Q16 舍入和帧边界采样。
func (s *State) Integrate(sub int) {
	if sub <= 0 {
		sub = 1
	}
	dt := s.frameDT.Div(fixed.FromInt(sub))
	gStep := s.gravity.Mul(dt)
	halfGravityDT2 := fixed.FromRaw(int(gStep.Mul(dt).Raw() / 2))
	for i := range s.posX {
		s.posX[i] = s.posX[i].Add(s.velX[i].Mul(dt))
		s.posY[i] = s.posY[i].Add(s.velY[i].Mul(dt)).Add(halfGravityDT2)
		s.velY[i] = s.velY[i].Add(gStep)
	}
}

// IntegrateSemiImplicit 保留一阶分量格式，用于验证截断误差方向：
//
//	vel_c += a_c*dt
//	pos_c += vel_c*dt
//
// 其累积位置误差为 O(dt)，且误差分量比等于重力分量比。
func (s *State) IntegrateSemiImplicit(sub int) {
	if sub <= 0 {
		sub = 1
	}
	dt := s.frameDT.Div(fixed.FromInt(sub))
	gStep := s.gravity.Mul(dt)
	for i := range s.posX {
		s.velY[i] = s.velY[i].Add(gStep)
		s.posX[i] = s.posX[i].Add(s.velX[i].Mul(dt))
		s.posY[i] = s.posY[i].Add(s.velY[i].Mul(dt))
	}
}

// Raw 按固定顺序导出全部数值，便于逐字节比较。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, 4*len(s.posX))
	for i := range s.posX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}
