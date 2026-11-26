package hash

import (
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

func TestHashOrderSensitive(t *testing.T) {
	a, _ := sim.NewStateWithLimit(2, 0, sim.MaxID)
	b, _ := sim.NewStateWithLimit(2, 0, sim.MaxID)
	a.Push(0, fixed.FromInt(1), 0)
	b.Push(1, fixed.FromInt(1), 0)
	if State(0, a) == State(0, b) {
		t.Fatal("顺序不同却同指纹")
	}
}

func TestHashStable(t *testing.T) {
	s := sim.NewState(3, fixed.FromInt(2))
	s.Integrate(1)
	if State(7, s) != State(7, s) {
		t.Fatal("同状态两次指纹不同")
	}
}

// TestHashIndependentOfAddRemoveOrder 构造两个增删历史不同、
// 但存活编号及其物理状态完全一致的世界，指纹必须相同。
func TestHashIndependentOfAddRemoveOrder(t *testing.T) {
	build := func(despawnEarly bool) *sim.State {
		s, _ := sim.NewStateWithLimit(0, 0, sim.MaxID)
		a, _ := s.Spawn() // 0
		tmp, _ := s.Spawn()
		b, _ := s.Spawn() // 2
		if despawnEarly {
			s.Despawn(tmp.ID)
		}
		c, _ := s.Spawn() // 3
		if !despawnEarly {
			s.Despawn(tmp.ID)
		}
		s.Push(a.ID, fixed.FromInt(1), 0)
		s.Push(b.ID, 0, fixed.FromInt(2))
		s.Push(c.ID, fixed.FromInt(-1), 0)
		s.Integrate(1)
		return s
	}
	h1 := State(1, build(true))
	h2 := State(1, build(false))
	if h1 != h2 {
		t.Fatalf("同编号同状态、仅增删时机不同却指纹不同: %016x != %016x", h1, h2)
	}
}
