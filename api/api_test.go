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

// TestInteractionCallOrderIndependent 同一组交互无论按什么调用顺序传入，
// StepWith 的末态哈希都必须相同；裸执行（不经过规范化）则不同。
func TestInteractionCallOrderIndependent(t *testing.T) {
	inter := []Interaction{
		{Tick: 0, Src: 0, Dst: 1, Gain: 1 << 16, Base: 0},
		{Tick: 0, Src: 1, Dst: 2, Gain: 1 << 16, Base: 0},
		{Tick: 0, Src: 2, Dst: 0, Gain: 1 << 16, Base: 0},
	}
	run := func(seq []Interaction) (uint64, []int32) {
		w, err := New(Options{Bodies: 3, SubSteps: 1})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if err := w.StepWith(nil, seq); err != nil {
				t.Fatal(err)
			}
		}
		return w.Hash(), w.Snapshot()
	}
	h1, s1 := run(inter)
	h2, s2 := run([]Interaction{inter[2], inter[0], inter[1]})
	h3, s3 := run([]Interaction{inter[1], inter[2], inter[0]})
	if h1 != h2 || h2 != h3 {
		t.Fatalf("调用顺序不同导致哈希不同: %016x %016x %016x", h1, h2, h3)
	}
	if !snapEqual(s1, s2) || !snapEqual(s2, s3) {
		t.Fatalf("哈希相同但快照不同:\n%v\n%v\n%v", s1, s2, s3)
	}
}

// TestCommandsAlsoCanonicalized 指令同样按实体编号规范化，乱序传入末态一致。
func TestCommandsAlsoCanonicalized(t *testing.T) {
	cmds := []Command{
		{Tick: 0, Body: 2, PushX: 1 << 14},
		{Tick: 0, Body: 0, PushX: 1 << 15},
		{Tick: 0, Body: 1, PushX: 1 << 16},
	}
	run := func(seq []Command) uint64 {
		w, _ := New(Options{Bodies: 3, SubSteps: 1})
		if err := w.StepWith(seq, nil); err != nil {
			t.Fatal(err)
		}
		return w.Hash()
	}
	if run(cmds) != run([]Command{cmds[2], cmds[0], cmds[1]}) {
		t.Fatal("指令乱序传入哈希不同")
	}
}

func snapEqual(a, b []int32) bool {
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
