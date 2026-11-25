// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import (
	"fmt"

	"goframe/internal/fixed"
)

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

// FromSlices 按固定顺序的原始定点值构造状态，长度不整除 4 时返回错误。
// 入参顺序必须与 Raw 一致：每实体依次 posX,posY,velX,velY；gravity 取原始定点值。
// 供对拍/指纹测试精确构造两个仅高位不同的状态，生产路径仍走 Push/Integrate。
func FromSlices(raw []int32, gravityRaw int32) (*State, error) {
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("字段数 %d 不是 4 的倍数", len(raw))
	}
	n := len(raw) / 4
	s := &State{
		posX:    make([]fixed.Value, n),
		posY:    make([]fixed.Value, n),
		velX:    make([]fixed.Value, n),
		velY:    make([]fixed.Value, n),
		gravity: fixed.FromRaw(int(gravityRaw)),
	}
	for i := 0; i < n; i++ {
		s.posX[i] = fixed.FromRaw(int(raw[4*i+0]))
		s.posY[i] = fixed.FromRaw(int(raw[4*i+1]))
		s.velX[i] = fixed.FromRaw(int(raw[4*i+2]))
		s.velY[i] = fixed.FromRaw(int(raw[4*i+3]))
	}
	return s, nil
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

// Raw 按固定顺序导出全部数值，便于逐字节比较。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, 4*len(s.posX))
	for i := range s.posX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}
