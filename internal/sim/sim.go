// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import (
	"errors"
	"fmt"

	"goframe/internal/fixed"
)

// ErrFieldID 表示字段编号或切片长度不合法。
var ErrFieldID = errors.New("字段编号非法")

// ErrFieldIDOutOfRange 构造编号越界错误。
func ErrFieldIDOutOfRange(id, n int) error {
	return fmt.Errorf("%w: id=%d, 字段总数=%d", ErrFieldID, id, n)
}

// ErrRawLengthMismatch 构造 Raw 长度不一致错误。
func ErrRawLengthMismatch(got, want int) error {
	return fmt.Errorf("%w: Raw 长度=%d, 应为 %d", ErrFieldID, got, want)
}

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

// Raw 按固定顺序导出全部数值，便于逐字节比较。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, 4*len(s.posX))
	for i := range s.posX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}

// ScalarCount 返回 Raw 顺序下的标量字段总数（每实体 4 个）。
func (s *State) ScalarCount() int { return 4 * len(s.posX) }

// SetScalar 按 Raw 的固定顺序设置第 id 个标量字段的原始定点值。
// 编号约定：id=4*i+k，k=0/1/2/3 分别对应 posX/posY/velX/velY。
// 差分回滚只回写差分条目中出现的编号，走的就是这个入口。
func (s *State) SetScalar(id int, raw int32) error {
	if id < 0 || id >= 4*len(s.posX) {
		return ErrFieldIDOutOfRange(id, 4*len(s.posX))
	}
	i, k := id/4, id%4
	v := fixed.FromRaw(int(raw))
	switch k {
	case 0:
		s.posX[i] = v
	case 1:
		s.posY[i] = v
	case 2:
		s.velX[i] = v
	default:
		s.velY[i] = v
	}
	return nil
}

// SetRaw 按 Raw 顺序整体写回，长度必须与 ScalarCount 一致。
func (s *State) SetRaw(vals []int32) error {
	if len(vals) != 4*len(s.posX) {
		return ErrRawLengthMismatch(len(vals), 4*len(s.posX))
	}
	for id, v := range vals {
		if err := s.SetScalar(id, v); err != nil {
			return err
		}
	}
	return nil
}
