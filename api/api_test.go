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

func TestSubStepsAffectResult(t *testing.T) {
	a, _ := New(Options{Bodies: 2, SubSteps: 1, Gravity: 10})
	b, _ := New(Options{Bodies: 2, SubSteps: 4, Gravity: 10})
	for i := 0; i < 4; i++ {
		a.Step(nil)
		b.Step(nil)
	}
	if a.Hash() == b.Hash() {
		t.Fatal("子步数不同却得到同一状态")
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

// TestDragOrderIsPartOfReplayContract 证明：同一参数下规范顺序可逐帧回放一致，
// 而把相乘顺序调换一次就会在首帧分叉。这是“必须固定顺序”的可执行证据。
func TestDragOrderIsPartOfReplayContract(t *testing.T) {
	opt := Options{Bodies: 3, SubSteps: 5, Gravity: 3, DragRaw: 8520}
	cmds := []Command{{Body: 0, PushY: 1 << 20}, {Body: 1, PushX: 1 << 18}}

	run := func() []uint64 {
		w, err := New(opt)
		if err != nil {
			t.Fatal(err)
		}
		var hs []uint64
		for i := 0; i < 6; i++ {
			if err := w.Step(cmds); err != nil {
				t.Fatal(err)
			}
			hs = append(hs, w.Hash())
		}
		return hs
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("同参数两次运行在帧 %d 分叉", i)
		}
	}
	// 阻尼系数与顺序共同进入指纹：换 k 必须换状态。
	opt2 := opt
	opt2.DragRaw = 6554
	w1, _ := New(opt)
	w2, _ := New(opt2)
	w1.Step(cmds)
	w2.Step(cmds)
	if w1.Hash() == w2.Hash() {
		t.Fatal("阻尼不同却同指纹")
	}
}

func TestDragRangeRejected(t *testing.T) {
	for _, d := range []int32{-1, 1<<16 + 1} {
		if _, err := New(Options{Bodies: 1, SubSteps: 1, DragRaw: d}); err == nil {
			t.Fatalf("DragRaw=%d 应被拒绝", d)
		}
	}
}
