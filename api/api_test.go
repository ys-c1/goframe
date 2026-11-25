package api

import "testing"

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

func TestSubStepsAreConsistent(t *testing.T) {
	a, _ := New(Options{Bodies: 2, SubSteps: 1, Gravity: 10})
	b, _ := New(Options{Bodies: 2, SubSteps: 4, Gravity: 10})
	for i := 0; i < 4; i++ {
		a.Step(nil)
		b.Step(nil)
	}
	// 分量精确常加速度格式在 1/4 子步可精确表示时应得到同一状态；
	// 旧实现只分摊重力、不缩放位移，才会让 SubSteps 改变结果。
	if a.Hash() != b.Hash() {
		t.Fatalf("子步数导致确定性状态不一致: %x vs %x", a.Hash(), b.Hash())
	}
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
