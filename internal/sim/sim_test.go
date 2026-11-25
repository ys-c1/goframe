package sim

import (
	"testing"

	"goframe/internal/fixed"
)

func TestPushOnlyTouchesOwnSlot(t *testing.T) {
	s := NewState(4, 0)
	s.ClearDirty()
	s.Push(2, fixed.FromInt(1), fixed.FromInt(-1))
	if d := s.Dirty(); len(d) != 1 || d[0] != 2 {
		t.Fatalf("Push 应只标脏目标槽位: %v", d)
	}
	for i := 0; i < 4; i++ {
		v := s.RawAt(i)
		want := [4]int32{}
		if i == 2 {
			want = [4]int32{0, 0, 1 << 16, -1 << 16}
		}
		if v != want {
			t.Fatalf("槽位 %d 被意外改写: got %v want %v", i, v, want)
		}
	}
}

func TestIntegrateOnlyTouchesOwnSlot(t *testing.T) {
	s := NewState(3, fixed.FromInt(4))
	before := make([][4]int32, 3)
	for i := range before {
		before[i] = s.RawAt(i)
	}
	s.ClearDirty()
	s.Integrate(2)
	for i := 0; i < 3; i++ {
		if s.RawAt(i) == before[i] {
			t.Fatalf("槽位 %d 在重力下应变化", i)
		}
	}
	if d := s.Dirty(); len(d) != 3 {
		t.Fatalf("三个槽位都应标脏: %v", d)
	}
}

func TestNoOpDoesNotDirty(t *testing.T) {
	s := NewState(4, 0)
	s.ClearDirty()
	s.Push(2, 0, 0)
	s.Integrate(1)
	if d := s.Dirty(); len(d) != 0 {
		t.Fatalf("无变化却标脏: %v", d)
	}
}

func TestDirtyDedup(t *testing.T) {
	s := NewState(4, 0)
	s.ClearDirty()
	s.Push(1, 1, 0)
	s.Push(1, 0, 1)
	s.Push(3, 1, 0)
	if d := s.Dirty(); len(d) != 2 || d[0] != 1 || d[1] != 3 {
		t.Fatalf("脏集合应去重且按首次标脏顺序: %v", d)
	}
}

// TestDirtyEpochWrap 验证世代号回绕后旧戳不会误判为新脏。
func TestDirtyEpochWrap(t *testing.T) {
	s := NewState(2, 0)
	s.ClearDirty()
	s.epoch = ^uint32(0) - 1 // 直接制造回绕前的边界
	s.Push(0, 1, 0)
	s.ClearDirty() // epoch 变为 ^uint32(0)
	if s.Epoch() != ^uint32(0) {
		t.Fatalf("epoch=%d", s.Epoch())
	}
	s.ClearDirty() // 回绕到 1，并清除旧戳
	if s.Epoch() != 1 {
		t.Fatalf("回绕后 epoch 应为 1，实际 %d", s.Epoch())
	}
	s.Push(1, 1, 0)
	if d := s.Dirty(); len(d) != 1 || d[0] != 1 {
		t.Fatalf("回绕后脏集合错误: %v", d)
	}
}
