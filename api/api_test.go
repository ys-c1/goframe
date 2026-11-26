package api

import (
	"errors"
	"testing"

	"goframe/internal/sim"
)

func TestDeterministic(t *testing.T) {
	opt := DefaultOptions()
	cmds := []Command{{Tick: 0, Body: 0, PushX: 1 << 16}, {Tick: 1, Body: 1, PushY: -1 << 15}}
	var hs []uint64
	for run := 0; run < 3; run++ {
		w, err := New(opt)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 8; i++ {
			if err := w.Step(cmds); err != nil {
				t.Fatal(err)
			}
		}
		hs = append(hs, w.Hash())
	}
	if hs[0] != hs[1] || hs[1] != hs[2] {
		t.Fatalf("非确定性: %v", hs)
	}
}

func TestSubStepsAffectResult(t *testing.T) {
	a, _ := New(Options{Bodies: 2, SubSteps: 1, Gravity: 10, SpawnsPerTick: 64})
	b, _ := New(Options{Bodies: 2, SubSteps: 4, Gravity: 10, SpawnsPerTick: 64})
	for i := 0; i < 4; i++ {
		a.Step(nil)
		b.Step(nil)
	}
	if a.Hash() == b.Hash() {
		t.Fatal("子步数不同却得到同一状态")
	}
}

func TestRejects(t *testing.T) {
	if _, err := New(Options{Bodies: -1, SubSteps: 1}); err == nil {
		t.Fatal("实体数为负应报错")
	}
	if _, err := New(Options{Bodies: 1, SubSteps: 1, SpawnsPerTick: 0}); err == nil {
		t.Fatal("每帧生成上限为零应报错")
	}
	w, _ := New(DefaultOptions())
	if err := w.Step([]Command{{Tick: 0, Body: 99}}); err == nil {
		t.Fatal("实体越界应报错")
	}
	if err := w.Step([]Command{{Tick: 0, Op: OpDespawn, Body: 99}}); err == nil {
		t.Fatal("销毁越界实体应报错")
	}
	if err := w.Step([]Command{{Tick: 0, Op: 9}}); err == nil {
		t.Fatal("非法指令应报错")
	}
}

// TestSpawnDespawnMonotonicID 验证编号单调递增、永不复用，
// 且销毁后再生成得到的是新编号而不是旧槽位。
func TestSpawnDespawnMonotonicID(t *testing.T) {
	w, err := New(Options{Bodies: 0, SubSteps: 1, SpawnsPerTick: 64})
	if err != nil {
		t.Fatal(err)
	}
	spawn := func() int {
		c := []Command{{Tick: w.Tick(), Op: OpSpawn, ID: SpawnUnallocated}}
		if err := w.Step(c); err != nil {
			t.Fatal(err)
		}
		return c[0].ID
	}
	id0 := spawn() // 0
	id1 := spawn() // 1
	if id0 != 0 || id1 != 1 {
		t.Fatalf("初始编号不是 0,1: %d,%d", id0, id1)
	}
	if err := w.Step([]Command{{Tick: w.Tick(), Op: OpDespawn, Body: id1}}); err != nil {
		t.Fatal(err)
	}
	id2 := spawn()
	if id2 != 2 {
		t.Fatalf("销毁后编号被复用: 得到 %d，期望 2", id2)
	}
	if w.State().Alive(1) {
		t.Fatal("已销毁实体仍存活")
	}
}

// TestSpawnCapPerTick 验证每帧生成数上限。
func TestSpawnCapPerTick(t *testing.T) {
	w, err := New(Options{Bodies: 0, SubSteps: 1, SpawnsPerTick: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = w.Step([]Command{
		{Tick: 0, Op: OpSpawn, ID: SpawnUnallocated},
		{Tick: 0, Op: OpSpawn, ID: SpawnUnallocated},
	})
	if !errors.Is(err, ErrBadOption) {
		t.Fatalf("超过每帧生成上限应报参数错，得到 %v", err)
	}
}

// TestIDExhausted 验证编号空间耗尽返回明确错误，且不回绕复用。
func TestIDExhausted(t *testing.T) {
	st, err := sim.NewStateWithLimit(0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Spawn(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Spawn(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Spawn(); !errors.Is(err, sim.ErrIDExhausted) {
		t.Fatalf("编号耗尽应返回 ErrIDExhausted，得到 %v", err)
	}
	if st.NextID() != 2 {
		t.Fatal("耗尽后编号游标被改动")
	}
}
