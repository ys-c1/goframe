// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import (
	"sort"

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

// VelX / VelY 读出实体速度，交互冲量据此计算，使冲量依赖于被共享的状态。
func (s *State) VelX(i int) fixed.Value { return s.velX[i] }
func (s *State) VelY(i int) fixed.Value { return s.velY[i] }

// Shove 是一次实体间交互：冲量 j = gain*vel(Src) + base，写入 Dst。
// 读集合 R={Src}，写集合 W={Dst}；Src==Dst 时为自作用，R∩W 非空。
type Shove struct {
	Src  int
	Dst  int
	Gain fixed.Value // 冲量对源速度的耦合系数
	Base fixed.Value // 状态无关的常量冲量部分
}

// Impulse 按当前状态算出本次交互实际施加的冲量（X、Y 分量同式）。
func (s *State) Impulse(sh Shove) fixed.Value {
	return s.velX[sh.Src].Mul(sh.Gain).Add(sh.Base)
}

// ApplyShove 执行一次交互：读 Src 速度算冲量，把冲量加到 Dst 速度。
// 冲量在同一步同时作用于 X、Y 两个分量，二者结构完全相同。
func (s *State) ApplyShove(sh Shove) {
	jx := s.velX[sh.Src].Mul(sh.Gain).Add(sh.Base)
	jy := s.velY[sh.Src].Mul(sh.Gain).Add(sh.Base)
	s.velX[sh.Dst] = s.velX[sh.Dst].Add(jx)
	s.velY[sh.Dst] = s.velY[sh.Dst].Add(jy)
}

// ApplyShoves 按调用方给定的顺序裸执行，顺序不同末态可能不同。
func (s *State) ApplyShoves(shoves []Shove) {
	for _, sh := range shoves {
		s.ApplyShove(sh)
	}
}

// key 是交互的全序键：实体编号在前，系数与常量冲量参与决胜，
// 保证任意两个不同交互可比较、相同交互多重集的规范化结果一致。
func (sh Shove) key() [4]int32 {
	return [4]int32{int32(sh.Src), int32(sh.Dst), sh.Gain.Raw(), sh.Base.Raw()}
}

// CanonicalShoves 返回按实体编号固定顺序排列的交互副本。
// 排序键是全序（含 Gain、Base 决胜），与调用顺序无关。
func CanonicalShoves(shoves []Shove) []Shove {
	out := append([]Shove(nil), shoves...)
	sort.SliceStable(out, func(a, b int) bool {
		ka, kb := out[a].key(), out[b].key()
		for i := range ka {
			if ka[i] != kb[i] {
				return ka[i] < kb[i]
			}
		}
		return false
	})
	return out
}

// ApplyShovesCanonical 先按实体编号规范化再执行，
// 同一组交互无论调用方按什么顺序传入，写入序列都相同。
func (s *State) ApplyShovesCanonical(shoves []Shove) {
	s.ApplyShoves(CanonicalShoves(shoves))
}
