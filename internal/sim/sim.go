// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import "goframe/internal/fixed"

// State 是所有实体的位置与速度，按固定顺序存放，便于逐字节比较。
type State struct {
	posX, posY []fixed.Value
	velX, velY []fixed.Value
	gravity    fixed.Value
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

// Clone 返回包含坐标与速度的完整独立拷贝，供预测端保存历史快照。
func (s *State) Clone() *State {
	copyVec := func(src []fixed.Value) []fixed.Value {
		out := make([]fixed.Value, len(src))
		copy(out, src)
		return out
	}
	return &State{
		posX:    copyVec(s.posX),
		posY:    copyVec(s.posY),
		velX:    copyVec(s.velX),
		velY:    copyVec(s.velY),
		gravity: s.gravity,
	}
}

// Push 给实体施加冲量。
func (s *State) Push(i int, dx, dy fixed.Value) {
	s.velX[i] = s.velX[i].Add(dx)
	s.velY[i] = s.velY[i].Add(dy)
}

// Integrate 做一次积分，sub 为总子步数，用于把重力分摊到子步。
func (s *State) Integrate(sub int) {
	g := s.gravity.Div(fixed.FromInt(sub))
	for i := range s.posX {
		s.velY[i] = s.velY[i].Add(g)
		s.posX[i] = s.posX[i].Add(s.velX[i])
		s.posY[i] = s.posY[i].Add(s.velY[i])
	}
}

// PosX 返回实体 i 的 X 坐标，供预测/纠正模块读取。
func (s *State) PosX(i int) fixed.Value { return s.posX[i] }

// PosY 返回实体 i 的 Y 坐标。
func (s *State) PosY(i int) fixed.Value { return s.posY[i] }

// VelX 返回实体 i 的 X 速度。
func (s *State) VelX(i int) fixed.Value { return s.velX[i] }

// VelY 返回实体 i 的 Y 速度。
func (s *State) VelY(i int) fixed.Value { return s.velY[i] }

// Raw 按固定顺序导出全部数值，便于逐字节比较。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, 4*len(s.posX))
	for i := range s.posX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}
