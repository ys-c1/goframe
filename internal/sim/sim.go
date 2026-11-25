// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import "goframe/internal/fixed"

// State 是所有实体的位置与速度，按固定顺序存放，便于逐字节比较。
//
// State 维护每个槽位的脏标记：Push 与 Integrate 只在该槽位四个数值确实
// 发生变化时才标脏。增量哈希依赖这份脏集合；只要调用方不绕过 State 的
// 方法直接改内存，脏集合就是“实际变化槽位”的精确集合。
type State struct {
	posX, posY []fixed.Value
	velX, velY []fixed.Value
	gravity    fixed.Value

	dirty []int    // 本世代变脏的槽位，按首次标脏顺序排列，无重复
	stamp []uint32 // stamp[i] == epoch 表示槽位 i 已在本世代标脏
	epoch uint32   // 世代号；ClearDirty 自增，O(1) 清空
}

// NewState 创建实体数为 n 的状态。
func NewState(n int, gravity fixed.Value) *State {
	return &State{
		posX:    make([]fixed.Value, n),
		posY:    make([]fixed.Value, n),
		velX:    make([]fixed.Value, n),
		velY:    make([]fixed.Value, n),
		gravity: gravity,
		stamp:   make([]uint32, n),
		epoch:   1,
	}
}

// Len 返回实体数。
func (s *State) Len() int { return len(s.posX) }

// RawAt 返回实体 i 的原始数值，顺序为 posX、posY、velX、velY。
// 哈希按这个固定顺序逐字节混合。
func (s *State) RawAt(i int) [4]int32 {
	return [4]int32{s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw()}
}

// Dirty 返回自上次 ClearDirty 以来发生过变化的槽位，每个槽位至多出现一次。
// 返回的切片由 State 持有，调用方不应保留到下一次修改之后。
func (s *State) Dirty() []int { return s.dirty }

// ClearDirty 开始新的脏世代，旧的标脏记录全部失效。O(1)。
func (s *State) ClearDirty() {
	s.epoch++
	s.dirty = s.dirty[:0]
	if s.epoch == 0 {
		// 世代号回绕：把旧世代的戳全部重置后再从 1 开始。
		for i := range s.stamp {
			s.stamp[i] = 0
		}
		s.epoch = 1
	}
}

func (s *State) markDirty(i int) {
	if s.stamp[i] == s.epoch {
		return
	}
	s.stamp[i] = s.epoch
	s.dirty = append(s.dirty, i)
}

// Epoch 返回当前脏世代号，仅供测试验证回绕。
func (s *State) Epoch() uint32 { return s.epoch }

// DebugSetRaw 直接覆盖实体 i 的四个数值，且不标脏。
//
// 仅供等价性测试构造“跨槽位影响但漏标”的违例。正常代码不应调用：
// 绕过脏标记会让增量哈希丢失这次变化，正是 doc.go 中反例所要展示的。
func (s *State) DebugSetRaw(i int, v [4]fixed.Value) {
	s.posX[i], s.posY[i], s.velX[i], s.velY[i] = v[0], v[1], v[2], v[3]
}

// Push 给实体施加冲量。只写实体 i 自己的速度槽位；数值没变则不标脏。
func (s *State) Push(i int, dx, dy fixed.Value) {
	nvx := s.velX[i].Add(dx)
	nvy := s.velY[i].Add(dy)
	if nvx == s.velX[i] && nvy == s.velY[i] {
		return
	}
	s.velX[i], s.velY[i] = nvx, nvy
	s.markDirty(i)
}

// Integrate 做一次积分，sub 为总子步数，用于把重力分摊到子步。
//
// 每个实体只用它自己的四个值计算下一状态，不同实体之间没有读写交叉，
// 因此一次 Integrate 只会影响各实体自己的槽位。没有发生变化的槽位不标脏。
func (s *State) Integrate(sub int) {
	g := s.gravity.Div(fixed.FromInt(sub))
	for i := range s.posX {
		nvx := s.velX[i]
		nvy := s.velY[i].Add(g)
		npx := s.posX[i].Add(nvx)
		npy := s.posY[i].Add(nvy)
		if nvx == s.velX[i] && nvy == s.velY[i] && npx == s.posX[i] && npy == s.posY[i] {
			continue
		}
		s.velX[i], s.velY[i] = nvx, nvy
		s.posX[i], s.posY[i] = npx, npy
		s.markDirty(i)
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
