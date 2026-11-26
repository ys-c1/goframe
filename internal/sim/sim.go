// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import (
	"errors"
	"sort"

	"goframe/internal/fixed"
)

// 编号空间上界与可回放最长时长挂钩：
//
//	60 帧/秒 × 86400 秒（连续 24 小时）× 每帧至多 64 次生成
//	= 331,776,000 < 2^31 = 2,147,483,647
//
// 即 int32 编号在最长回放段内不可能耗尽，仍留有余量。
const MaxID = 331_776_000

// SlotWords 是每个实体在哈希输入流中占据的定长槽位字数：
// posX、posY、velX、velY 四个 Q16.16 字。
const SlotWords = 4

// ErrIDExhausted 表示单调编号空间已耗尽。
var ErrIDExhausted = errors.New("实体编号空间耗尽")

// Body 是单个实体的物理状态。实体身份由全局单调递增、永不复用的
// 编号 ID 决定，与它在存储中的槽位无关。
type Body struct {
	ID         int
	PosX, PosY fixed.Value
	VelX, VelY fixed.Value
}

// State 以编号为键稀疏存放实体。增删实体只改动映射本身，
// 不会挪动任何存活实体的槽位，因此存活实体的编号->状态映射
// 与增删顺序无关。
type State struct {
	bodies  map[int]*Body
	nextID  int
	limit   int
	gravity fixed.Value
}

// NewState 创建初始实体数为 n 的状态，初始编号为 0..n-1，
// 使用默认编号上界 MaxID。
func NewState(n int, gravity fixed.Value) *State {
	s, err := NewStateWithLimit(n, gravity, MaxID)
	if err != nil {
		panic(err) // 默认上界下 n >= 0 不可能失败
	}
	return s
}

// NewStateWithLimit 允许指定编号上界，供编号耗尽测试使用。
func NewStateWithLimit(n int, gravity fixed.Value, limit int) (*State, error) {
	if n < 0 || limit <= 0 || n > limit {
		return nil, errors.New("非法初始实体数或编号上界")
	}
	s := &State{bodies: make(map[int]*Body, n), limit: limit, gravity: gravity}
	for i := 0; i < n; i++ {
		s.bodies[i] = &Body{ID: i}
	}
	s.nextID = n
	return s, nil
}

// Len 返回存活实体数。
func (s *State) Len() int { return len(s.bodies) }

// NextID 返回下一个将被分配的编号。
func (s *State) NextID() int { return s.nextID }

// Spawn 分配一个全局单调递增、永不复用的编号并创建实体。
// 编号耗尽时返回 ErrIDExhausted：此时不得回绕复用旧编号，
// 应结束当前回放段、滚动开启新段并把 nextID 归零（段号混入指纹）。
func (s *State) Spawn() (*Body, error) {
	if s.nextID >= s.limit {
		return nil, ErrIDExhausted
	}
	b := &Body{ID: s.nextID}
	s.bodies[b.ID] = b
	s.nextID++
	return b, nil
}

// Despawn 销毁实体并释放其稀疏槽位，编号不回收、不复用。
// 实体不存在时返回 false。
func (s *State) Despawn(id int) bool {
	if _, ok := s.bodies[id]; !ok {
		return false
	}
	delete(s.bodies, id)
	return true
}

// Alive 判断编号是否仍存活。
func (s *State) Alive(id int) bool {
	_, ok := s.bodies[id]
	return ok
}

// BodyAt 按编号取实体，不存在返回 nil。
func (s *State) BodyAt(id int) *Body { return s.bodies[id] }

// Push 给实体施加冲量，实体已销毁时静默忽略。
func (s *State) Push(id int, dx, dy fixed.Value) {
	b := s.bodies[id]
	if b == nil {
		return
	}
	b.VelX = b.VelX.Add(dx)
	b.VelY = b.VelY.Add(dy)
}

// Integrate 做一次积分，sub 为总子步数，用于把重力分摊到子步。
// 始终按编号升序遍历，遍历顺序与增删历史无关。
func (s *State) Integrate(sub int) {
	g := s.gravity.Div(fixed.FromInt(sub))
	for _, id := range s.sortedIDs() {
		b := s.bodies[id]
		b.VelY = b.VelY.Add(g)
		b.PosX = b.PosX.Add(b.VelX)
		b.PosY = b.PosY.Add(b.VelY)
	}
}

func (s *State) sortedIDs() []int {
	ids := make([]int, 0, len(s.bodies))
	for id := range s.bodies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// SortedBodies 按编号升序返回存活实体，供指纹按固定顺序混合。
func (s *State) SortedBodies() []*Body {
	ids := s.sortedIDs()
	out := make([]*Body, len(ids))
	for i, id := range ids {
		out[i] = s.bodies[id]
	}
	return out
}

// Raw 按编号升序导出全部数值，每实体 SlotWords 个字。
// 顺序只取决于编号，不取决于增删顺序或存储槽位。
func (s *State) Raw() []int32 {
	bs := s.SortedBodies()
	out := make([]int32, 0, SlotWords*len(bs))
	for _, b := range bs {
		out = append(out, b.PosX.Raw(), b.PosY.Raw(), b.VelX.Raw(), b.VelY.Raw())
	}
	return out
}
