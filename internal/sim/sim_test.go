package sim

import (
	"sort"
	"testing"

	"goframe/internal/fixed"
)

// brokenKeySort 只用 (Src,Dst) 排序、不做决胜，模拟全序键被去掉决胜字段。
func brokenKeySort(in []Shove) []Shove {
	out := append([]Shove(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Src != out[j].Src {
			return out[i].Src < out[j].Src
		}
		return out[i].Dst < out[j].Dst
	})
	return out
}

// TestCanonicalShovesOrderIndependent 同一组交互任意调用顺序，
// 规范化后的写入序列必须完全一致。
func TestCanonicalShovesOrderIndependent(t *testing.T) {
	shoves := []Shove{
		{Src: 2, Dst: 0, Gain: fixed.FromInt(1), Base: 0},
		{Src: 0, Dst: 1, Gain: fixed.FromInt(1), Base: 0},
		{Src: 1, Dst: 2, Gain: fixed.FromInt(1), Base: 0},
	}
	want := CanonicalShoves(shoves)
	// 一个与初始顺序不同的排列。
	rotated := []Shove{shoves[1], shoves[2], shoves[0]}
	got := CanonicalShoves(rotated)
	if len(got) != len(want) {
		t.Fatalf("长度不一致 %d != %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("位置 %d 规范化不一致: %+v != %+v", i, got[i], want[i])
		}
	}
}

// TestBareOrderDependsOnSharedWrite 裸执行时，共享写入使不同顺序产生不同末态，
// 且差异恰好来自交互冲量的改变（冲量差）。
func TestBareOrderDependsOnSharedWrite(t *testing.T) {
	newWorld := func() *State {
		s := NewState(2, 0)
		s.Push(0, fixed.FromInt(3), fixed.FromInt(3))
		s.Push(1, fixed.FromInt(5), fixed.FromInt(5))
		return s
	}
	ab := []Shove{
		{Src: 0, Dst: 1, Gain: fixed.FromInt(1), Base: 0},
		{Src: 1, Dst: 0, Gain: fixed.FromInt(1), Base: 0},
	}
	ba := []Shove{ab[1], ab[0]}

	s1, s2 := newWorld(), newWorld()
	// I0 先执行时冲量=gain*v0=3；调换后 I1 先把 v0 推到 5，I0 冲量变为 5，冲量差=2。
	jFirst := s1.Impulse(ab[0]).Raw() // 顺序 I0->I1：I0 在第 0 位
	s1.ApplyShoves(ab)
	jSecond := s2.Impulse(ba[0]).Raw() // 顺序 I1->I0：I0 在第 0 位
	s2.ApplyShoves(ba)
	if jFirst == jSecond {
		t.Fatal("同一交互在两种顺序下冲量相同，共享写入未生效")
	}
	if jFirst != fixed.FromInt(3).Raw() || jSecond != fixed.FromInt(5).Raw() {
		t.Fatalf("冲量差不符: first=%d second=%d, 期望 196608(=3) 与 327680(=5)", jFirst, jSecond)
	}
	if eq := rawEqual(s1.Raw(), s2.Raw()); eq {
		t.Fatal("裸执行不同顺序末态却相同")
	}

	// 规范化后两种调用顺序必须收敛到同一末态。
	c1, c2 := newWorld(), newWorld()
	c1.ApplyShovesCanonical(ab)
	c2.ApplyShovesCanonical(ba)
	if !rawEqual(c1.Raw(), c2.Raw()) {
		t.Fatalf("规范化后末态不一致:\n%v\n%v", c1.Raw(), c2.Raw())
	}
}

// TestDisjointWritesCommute 充要条件的正面验证：写集合不相交时，
// 即便读集合与对方写集合交叠，裸执行也与顺序无关。
func TestDisjointWritesCommute(t *testing.T) {
	shoves := []Shove{
		{Src: 0, Dst: 2, Gain: fixed.FromInt(1), Base: fixed.FromInt(7)},
		{Src: 1, Dst: 3, Gain: fixed.FromInt(1), Base: fixed.FromInt(5)},
	}
	mk := func() *State {
		s := NewState(4, 0)
		for i := 0; i < 4; i++ {
			s.Push(i, fixed.FromInt(i+1), fixed.FromInt(i+1))
		}
		return s
	}
	a, b := mk(), mk()
	a.ApplyShoves(shoves)
	b.ApplyShoves([]Shove{shoves[1], shoves[0]})
	if !rawEqual(a.Raw(), b.Raw()) {
		t.Fatalf("写集合不相交却受顺序影响:\n%v\n%v", a.Raw(), b.Raw())
	}
}

// TestTieBreakRequired 全序键去掉决胜字段后，同键交互保留输入相对顺序，
// 唯一性失效；含决胜字段的全序键则收敛。
func TestTieBreakRequired(t *testing.T) {
	shoves := []Shove{
		{Src: 0, Dst: 0, Gain: fixed.FromInt(1), Base: 0},
		{Src: 0, Dst: 0, Gain: fixed.FromInt(2), Base: fixed.FromInt(1)},
	}
	mk := func() *State {
		s := NewState(1, 0)
		s.Push(0, fixed.FromInt(1), fixed.FromInt(1))
		return s
	}
	// 全序键：两种输入顺序同一末态。
	a, b := mk(), mk()
	a.ApplyShovesCanonical(shoves)
	b.ApplyShovesCanonical([]Shove{shoves[1], shoves[0]})
	if !rawEqual(a.Raw(), b.Raw()) {
		t.Fatal("全序键未能规范化同键交互")
	}
	// 残缺键（仅 Src,Dst，无决胜）：稳定排序保留输入顺序，末态不同。
	c, d := mk(), mk()
	c.ApplyShoves(brokenKeySort(shoves))
	d.ApplyShoves(brokenKeySort([]Shove{shoves[1], shoves[0]}))
	if rawEqual(c.Raw(), d.Raw()) {
		t.Fatal("残缺键竟然仍唯一，反例失效")
	}
}

func rawEqual(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
