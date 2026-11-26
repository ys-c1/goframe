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

// Clone 返回状态的深拷贝，回滚引擎用它保存帧快照。
func (s *State) Clone() *State {
	cp := &State{gravity: s.gravity}
	cp.posX = append(cp.posX, s.posX...)
	cp.posY = append(cp.posY, s.posY...)
	cp.velX = append(cp.velX, s.velX...)
	cp.velY = append(cp.velY, s.velY...)
	return cp
}

// Load 用 Raw 导出的顺序写回全部数值，用于回滚恢复。
// 长度不匹配时直接返回，不做部分写入。
func (s *State) Load(raw []int32) {
	n := len(s.posX)
	if len(raw) != 4*n {
		return
	}
	for i := 0; i < n; i++ {
		s.posX[i] = fixed.FromRaw(int(raw[4*i]))
		s.posY[i] = fixed.FromRaw(int(raw[4*i+1]))
		s.velX[i] = fixed.FromRaw(int(raw[4*i+2]))
		s.velY[i] = fixed.FromRaw(int(raw[4*i+3]))
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
