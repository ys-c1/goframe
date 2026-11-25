// Package hash 提供状态指纹：全量指纹与逐位等价的增量指纹。
// 定义、等价条件与逐位等价的证明见 doc.go。
package hash

import "goframe/internal/sim"

const (
	offset = 14695981039346656037
	prime  = 1099511628211

	// slotWidth 是一个实体在混合序列里占用的 32 位字个数：posX、posY、velX、velY。
	slotWidth = 4
)

// p4 是槽位宽度个 prime 之积，即 P = p^4（模 2^64）。用运行时乘法求值，
// 因为常量化会按无限精度精确计算而溢出 uint64。
var p4 = func() uint64 {
	p := uint64(prime)
	return p * p * p * p
}()

// fold 把一个字混入槽位摘要：h ← (h + y) * p。
func fold(h, y uint64) uint64 {
	return (h + y) * prime
}

// slotDigest 计算实体 i 的槽位摘要 D_i。只依赖 i 与该实体自己的四个字。
func slotDigest(raw [slotWidth]int32, i int) uint64 {
	h := uint64(offset) + uint64(i)*prime
	for _, w := range raw {
		h = fold(h, uint64(uint32(w)))
	}
	return h
}

// slotWeight 返回实体 i 的权重 W_i = p * P^(n-1-i)，P = p^4。
// powP 存的是 p 的各次幂，故指数为 slotWidth*(n-1-i)。
func slotWeight(i, n int, powP []uint64) uint64 {
	return prime * powP[slotWidth*(n-1-i)]
}

// primePowers 返回 p^0..p^m。
func primePowers(m int) []uint64 {
	out := make([]uint64, m+1)
	out[0] = 1
	for i := 1; i <= m; i++ {
		out[i] = out[i-1] * prime
	}
	return out
}

// State 返回帧号与状态的指纹（全量）。不依赖任何历史，逐位可由状态唯一确定。
//
// 求和与权重在同一趟里自尾向头完成，不分配内存。
func State(tick int, s *sim.State) uint64 {
	n := s.Len()
	var h uint64
	w := uint64(prime) // W_{n-1} = p
	for i := n - 1; i >= 0; i-- {
		h += w * slotDigest(s.RawAt(i), i)
		w *= p4 // 进入下一项时 W_{i-1} = W_i * P
	}
	// 循环结束后 w = p * P^n，恰是帧号项 Q(t) 中 (o + t) 的系数。
	return h + w*(uint64(offset)+uint64(uint32(tick)))
}

// Incremental 维护与全量逐位相同的指纹中间值。
//
// 成立前提见 doc.go 的等价条件 (C)：一次更新只改被更新实体自己的槽位，
// 且变化都被 State 标脏。绕过 State 方法直接改内存会破坏前提，此时
// Check 能逐位暴露差异。
type Incremental struct {
	s      *sim.State
	n      int
	powP   []uint64
	weight []uint64 // weight[i] = W_i = p * P^(n-1-i)
	dig    []uint64 // 缓存的 D_i
	sum    uint64   // Σ W_i * D_i
	q      uint64   // Q(tick)
}

// NewIncremental 在 s 的当前状态上建立增量哈希，调用方应紧接着 SetTick。
func NewIncremental(s *sim.State) *Incremental {
	n := s.Len()
	inc := &Incremental{
		s:      s,
		n:      n,
		powP:   primePowers(slotWidth*n + 1),
		weight: make([]uint64, n),
		dig:    make([]uint64, n),
	}
	for i := 0; i < n; i++ {
		inc.weight[i] = slotWeight(i, n, inc.powP)
		inc.dig[i] = slotDigest(s.RawAt(i), i)
		inc.sum += inc.weight[i] * inc.dig[i]
	}
	inc.SetTick(0)
	return inc
}

// SetTick 更新帧号项 Q(t)。O(1)。
func (inc *Incremental) SetTick(tick int) {
	inc.q = prime * inc.powP[slotWidth*inc.n] * (uint64(offset) + uint64(uint32(tick)))
}

// Update 只重算脏槽位并修补部分和，返回当前增量指纹，随后清空脏集合。
//
// 脏集合自上次 Update 起累积；即使中间跨了多帧、多次修改，缓存的 D_i 仍是
// 上一世代的值，差值 ΔD 依然精确，因此跨帧调用也逐位正确。
func (inc *Incremental) Update() uint64 {
	for _, i := range inc.s.Dirty() {
		nd := slotDigest(inc.s.RawAt(i), i)
		if nd == inc.dig[i] {
			continue // 多标脏：摘要未变，加零
		}
		inc.sum += inc.weight[i] * (nd - inc.dig[i])
		inc.dig[i] = nd
	}
	inc.s.ClearDirty()
	return inc.Value()
}

// Value 以缓存值返回当前增量指纹，不做任何重算。
func (inc *Incremental) Value() uint64 { return inc.q + inc.sum }

// Check 把增量内部状态与全量逐位比较一次。
//
// SlotDiffs 是按当前状态重算的 D_i 与缓存不一致的槽位数，BrokenSlots 列出
// 这些槽位；ProductDiff 是部分和层面的差异，HashDiff 是最终 64 位值的差异
// （各为 0 或 1）。三项全为 0 即逐位等价。
// Check 不修改缓存，也不清空脏集合。调用方应先 SetTick 再 Check。
func (inc *Incremental) Check(tick int) Equivalence {
	eq := Equivalence{Full: State(tick, inc.s), Inc: inc.Value()}
	var freshSum uint64
	for i := 0; i < inc.n; i++ {
		d := slotDigest(inc.s.RawAt(i), i)
		if d != inc.dig[i] {
			eq.SlotDiffs++
			eq.BrokenSlots = append(eq.BrokenSlots, i)
		}
		freshSum += inc.weight[i] * d
	}
	if inc.sum != freshSum {
		eq.ProductDiff = 1
	}
	if eq.Full != eq.Inc {
		eq.HashDiff = 1
	}
	return eq
}

// Equivalence 是一次 Check 的结果。
type Equivalence struct {
	SlotDiffs   int   // 缓存 D_i 与重算 D_i 不一致的槽位数
	BrokenSlots []int // 具体槽位
	ProductDiff int   // 部分和是否不一致：0 一致，1 不一致
	HashDiff    int   // 64 位终值是否不一致：0 一致，1 不一致
	Full        uint64
	Inc         uint64
}

// Equal 报告是否逐位等价（三项差异都为 0）。
func (e Equivalence) Equal() bool {
	return e.SlotDiffs == 0 && e.ProductDiff == 0 && e.HashDiff == 0
}
