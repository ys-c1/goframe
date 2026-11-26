package alloc

import (
	"sort"

	"goframe/internal/fixed"
)

const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
	slotWords = 4
)

func fnvHash(tick int, words []int32) uint64 {
	h := uint64(fnvOffset)
	h ^= uint64(uint32(tick))
	h *= fnvPrime
	for _, v := range words {
		h ^= uint64(uint32(v))
		h *= fnvPrime
	}
	return h
}

// slotBody 是定长槽位里的一个实体。
type slotBody struct {
	posX, posY fixed.Value
	velX, velY fixed.Value
}

// Recycled 是旧策略：定长可增长槽位数组 + LIFO 自由表，
// 销毁后的槽位编号会被后续生成复用。槽位按存储顺序入哈希流。
type Recycled struct {
	slots    []slotBody // 空洞位置保持零值，模拟未缩容的定长状态
	free     []int      // LIFO 自由表
	nameSlot map[string]int
	spawns   int // 累计生成次数
	reuses   int // 其中命中自由表（复用槽位）的次数
}

func NewRecycled() *Recycled {
	return &Recycled{nameSlot: map[string]int{}}
}

func (r *Recycled) spawn(name string) int {
	r.spawns++
	if n := len(r.free); n > 0 {
		slot := r.free[n-1]
		r.free = r.free[:n-1]
		r.reuses++
		r.nameSlot[name] = slot
		return slot
	}
	slot := len(r.slots)
	r.slots = append(r.slots, slotBody{})
	r.nameSlot[name] = slot
	return slot
}

func (r *Recycled) despawn(name string) {
	slot := r.nameSlot[name]
	delete(r.nameSlot, name)
	// 释放即清零：与 sim 旧实现的定长零值槽位一致。
	r.slots[slot] = slotBody{}
	r.free = append(r.free, slot)
}

func (r *Recycled) push(name string, x, y int32) {
	b := &r.slots[r.nameSlot[name]]
	b.velX = b.velX.Add(fixed.FromRaw(int(x)))
	b.velY = b.velY.Add(fixed.FromRaw(int(y)))
}

func (r *Recycled) integrate() {
	for i := range r.slots {
		r.slots[i].posX = r.slots[i].posX.Add(r.slots[i].velX)
		r.slots[i].posY = r.slots[i].posY.Add(r.slots[i].velY)
	}
}

// Raw 按存储槽位顺序导出全部字（含空洞零值）。
func (r *Recycled) Raw() []int32 {
	out := make([]int32, 0, slotWords*len(r.slots))
	for i := range r.slots {
		b := &r.slots[i]
		out = append(out, b.posX.Raw(), b.posY.Raw(), b.velX.Raw(), b.velY.Raw())
	}
	return out
}

func (r *Recycled) hash(tick int) uint64 { return fnvHash(tick, r.Raw()) }

// highWater 是槽位数组历史峰值：总生成数 - 复用次数。
// 因此 复用次数 = 总生成数 - 高水位，与实现细节无关、可独立核验。
func (r *Recycled) highWater() int { return r.spawns - r.reuses }

// slotOf 返回逻辑实体最终所在的存储槽位。
func (r *Recycled) slotOf(name string) int { return r.nameSlot[name] }

// MonoBody 是单调编号引擎里的实体。
type MonoBody struct {
	id         int
	posX, posY fixed.Value
	velX, velY fixed.Value
}

// Monotonic 是新策略：编号全局单调递增、永不复用，
// 哈希按编号升序混合且把编号本身入流。
type Monotonic struct {
	bodies map[int]*MonoBody
	nameID map[string]int
	nextID int
	spawns int
}

func NewMonotonic() *Monotonic {
	return &Monotonic{bodies: map[int]*MonoBody{}, nameID: map[string]int{}}
}

func (m *Monotonic) spawn(name string) int {
	id := m.nextID
	m.nextID++
	m.spawns++
	m.bodies[id] = &MonoBody{id: id}
	m.nameID[name] = id
	return id
}

func (m *Monotonic) despawn(name string) {
	id := m.nameID[name]
	delete(m.nameID, name)
	delete(m.bodies, id) // 编号不回收
}

func (m *Monotonic) push(name string, x, y int32) {
	b := m.bodies[m.nameID[name]]
	b.velX = b.velX.Add(fixed.FromRaw(int(x)))
	b.velY = b.velY.Add(fixed.FromRaw(int(y)))
}

func (m *Monotonic) integrate() {
	ids := make([]int, 0, len(m.bodies))
	for id := range m.bodies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		b := m.bodies[id]
		b.posX = b.posX.Add(b.velX)
		b.posY = b.posY.Add(b.velY)
	}
}

// Hash 与 internal/hash.State 的混合规则一致：编号 + 四个状态字。
func (m *Monotonic) Hash(tick int) uint64 {
	ids := make([]int, 0, len(m.bodies))
	for id := range m.bodies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	h := uint64(fnvOffset)
	mix := func(v uint64) {
		h ^= v
		h *= fnvPrime
	}
	mix(uint64(uint32(tick)))
	for _, id := range ids {
		b := m.bodies[id]
		mix(uint64(uint32(id)))
		mix(uint64(uint32(b.posX.Raw())))
		mix(uint64(uint32(b.posY.Raw())))
		mix(uint64(uint32(b.velX.Raw())))
		mix(uint64(uint32(b.velY.Raw())))
	}
	return h
}

func (m *Monotonic) idOf(name string) int { return m.nameID[name] }

// Raw 按编号升序导出存活实体，每实体 5 字（编号 + 四个状态字）。
func (m *Monotonic) Raw() []int32 {
	ids := make([]int, 0, len(m.bodies))
	for id := range m.bodies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]int32, 0, 5*len(ids))
	for _, id := range ids {
		b := m.bodies[id]
		out = append(out, int32(id), b.posX.Raw(), b.posY.Raw(), b.velX.Raw(), b.velY.Raw())
	}
	return out
}
