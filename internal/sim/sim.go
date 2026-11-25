// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import "goframe/internal/fixed"

// State 是所有实体的位置与速度，按固定顺序存放，便于逐字节比较。
type State struct {
	posX, posY []fixed.Value
	velX, velY []fixed.Value
	gravity    fixed.Value
	drag       fixed.Value // 每帧阻尼系数 k，0 为无阻尼
	dragCoef   fixed.Value // 依子步长算出的三层系数 k*dt，供 v*(k*dt) 复用
}

// NewState 创建实体数为 n 的状态。阻尼默认关闭（k=0），
// 因此不调用 SetDrag 的运行结果与旧版本逐字节相同。
func NewState(n int, gravity fixed.Value) *State {
	return &State{
		posX:    make([]fixed.Value, n),
		posY:    make([]fixed.Value, n),
		velX:    make([]fixed.Value, n),
		velY:    make([]fixed.Value, n),
		gravity: gravity,
	}
}

// SetDrag 设置每帧阻尼系数 k，取值 [0,1]，0 为无阻尼。
func (s *State) SetDrag(k fixed.Value) { s.drag = k }

// Drag 返回每帧阻尼系数。
func (s *State) Drag() fixed.Value { return s.drag }

// Len 返回实体数。
func (s *State) Len() int { return len(s.posX) }

// Push 给实体施加冲量。
func (s *State) Push(i int, dx, dy fixed.Value) {
	s.velX[i] = s.velX[i].Add(dx)
	s.velY[i] = s.velY[i].Add(dy)
}

// Integrate 做一次积分，sub 为每帧子步数，用于把重力与阻尼分摊到子步。
//
// 每子步的阻尼冲量 v*(k*dt) 是三因子乘积，顺序固定为先结合 k*dt，
// 再与速度 v 相乘（fixed.Mul3 的右结合约定）。k*dt 每帧只算一次并让所有
// 实体共用；若改写成 (v*k)*dt，各实体的结果会因取整误差与回放指纹分叉，
// 详见 docs/mul-associativity.md。
func (s *State) Integrate(sub int) {
	dt := fixed.One.Div(fixed.FromInt(sub)) // 子步长 1/sub
	g := s.gravity.Div(fixed.FromInt(sub))  // 每子步重力增量
	s.dragCoef = fixed.Mul3Coef(s.drag, dt) // k*dt，顺序约定的第一步
	for i := range s.posX {
		// 阻尼冲量按旧速度算，两轴一致；随后施加重力，最后积分位置。
		dragX := fixed.Mul3By(s.velX[i], s.dragCoef)
		dragY := fixed.Mul3By(s.velY[i], s.dragCoef)
		s.velX[i] = s.velX[i].Sub(dragX)
		s.velY[i] = s.velY[i].Add(g)
		s.velY[i] = s.velY[i].Sub(dragY)
		s.posX[i] = s.posX[i].Add(s.velX[i])
		s.posY[i] = s.posY[i].Add(s.velY[i])
	}
}

// DragCoef 返回最近一次 Integrate 使用的系数 k*dt，仅供检视。
func (s *State) DragCoef() fixed.Value { return s.dragCoef }

// Raw 按固定顺序导出全部数值，便于逐字节比较。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, 4*len(s.posX))
	for i := range s.posX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}
