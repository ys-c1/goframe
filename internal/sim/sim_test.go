package sim

import (
	"errors"
	"testing"

	"goframe/internal/fixed"
)

func TestSpawnDespawnDoesNotMoveOthers(t *testing.T) {
	a, _ := NewStateWithLimit(0, 0, MaxID)
	b0, _ := a.Spawn()
	b1, _ := a.Spawn()
	a.Push(b0.ID, fixed.FromInt(3), 0)
	a.Despawn(b1.ID)
	b2, _ := a.Spawn() // 新编号 2，绝不复用 1
	if b2.ID != 2 {
		t.Fatalf("期望新编号 2，得到 %d", b2.ID)
	}
	if got := a.BodyAt(0).VelX; got != fixed.FromInt(3) {
		t.Fatalf("存活实体状态被增删挪动: %v", got)
	}
}

func TestExhaustionNoWraparound(t *testing.T) {
	s, _ := NewStateWithLimit(0, 0, 1)
	if _, err := s.Spawn(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spawn(); !errors.Is(err, ErrIDExhausted) {
		t.Fatalf("期望 ErrIDExhausted，得到 %v", err)
	}
}

func TestRawIsIDSorted(t *testing.T) {
	s, _ := NewStateWithLimit(0, 0, MaxID)
	b0, _ := s.Spawn()
	b1, _ := s.Spawn()
	b2, _ := s.Spawn()
	s.Despawn(b1.ID) // 留下编号空洞
	raw := s.Raw()
	if len(raw) != SlotWords*2 {
		t.Fatalf("空洞实体不应入流，得到长度 %d", len(raw))
	}
	if raw[0] != b0.PosX.Raw() {
		t.Fatal("首字必须是最小编号实体")
	}
	_ = b2
}
