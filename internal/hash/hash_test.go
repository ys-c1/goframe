package hash

import (
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

func TestHashOrderSensitive(t *testing.T) {
	a := sim.NewState(2, 0)
	b := sim.NewState(2, 0)
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
