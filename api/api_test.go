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

func TestPerturbIsDeterministicAndActive(t *testing.T) {
	opt := Options{Bodies: 8, SubSteps: 2, Gravity: 3, Seed: 20261126, PerturbAmp: 1 << 14}

	run := func() []uint64 {
		w, err := New(opt)
		if err != nil {
			t.Fatal(err)
		}
		hs := make([]uint64, 16)
		for i := range hs {
			if err := w.Step(nil); err != nil {
				t.Fatal(err)
			}
			hs[i] = w.Hash()
		}
		return hs
	}

	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("同种子两次运行在 tick=%d 指纹不同", i)
		}
	}

	// 扰动必须真的改变状态：与关闭扰动的世界至少有一帧指纹不同。
	off := opt
	off.PerturbAmp = 0
	w0, _ := New(off)
	changed := false
	for i := 0; i < 16; i++ {
		if err := w0.Step(nil); err != nil {
			t.Fatal(err)
		}
		if w0.Hash() != a[i] {
			changed = true
		}
	}
	if !changed {
		t.Fatal("开启 PerturbAmp 后状态没有任何变化")
	}
}

func TestPerturbRejectsNegativeAmp(t *testing.T) {
	if _, err := New(Options{Bodies: 1, SubSteps: 1, PerturbAmp: -1}); err == nil {
		t.Fatal("负扰动幅度应报错")
	}
}
