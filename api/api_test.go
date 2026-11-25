package api

import (
	"testing"

	"goframe/internal/fixed"
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

// TestSubStepsAffectResult 同时钉住两件事：
//  1. 保留下来的旧 Euler 参考内核确实仍与子步数相关（这是原 bug 的证明）；
//  2. 新的中点内核在 1 档与 4 档下末态哈希必须相同（修复目标）。
func TestSubStepsAffectResult(t *testing.T) {
	// (1) 旧 Euler：1 档与 4 档必须不同。
	e1 := sim.NewState(2, fixed.FromInt(10))
	e4 := sim.NewState(2, fixed.FromInt(10))
	for i := 0; i < 4; i++ {
		e1.IntegrateEuler(1)
	}
	for i := 0; i < 4; i++ {
		e4.IntegrateEuler(4)
	}
	if goframeProofEqualRaw(e1.Raw(), e4.Raw()) {
		t.Fatal("旧 Euler 内核在 1/4 子步下竟相同，相关性证明失效")
	}

	// (2) 新中点内核：1 档与 4 档必须相同。
	a, _ := New(Options{Bodies: 2, SubSteps: 1, Gravity: 10})
	b, _ := New(Options{Bodies: 2, SubSteps: 4, Gravity: 10})
	cmds := []Command{{Tick: 0, Body: 0, PushX: 12345}, {Tick: 0, Body: 1, PushY: -9876}}
	for i := 0; i < 6; i++ {
		if err := a.Step(cmds); err != nil {
			t.Fatal(err)
		}
		if err := b.Step(cmds); err != nil {
			t.Fatal(err)
		}
	}
	if a.Hash() != b.Hash() {
		t.Fatalf("中点内核子步数无关性被破坏: %016x vs %016x", a.Hash(), b.Hash())
	}
}

func goframeProofEqualRaw(a, b []int32) bool {
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

func TestRejects(t *testing.T) {
	if _, err := New(Options{Bodies: 0, SubSteps: 1}); err == nil {
		t.Fatal("实体数为零应报错")
	}
	w, _ := New(DefaultOptions())
	if err := w.Step([]Command{{Tick: 0, Body: 99}}); err == nil {
		t.Fatal("实体越界应报错")
	}
}
