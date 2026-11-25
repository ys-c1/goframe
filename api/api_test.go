package api

import (
	"testing"
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

// TestIncrementalMatchesFullAcrossStep 在 API 层逐帧比较两条指纹，要求逐位相同。
func TestIncrementalMatchesFullAcrossStep(t *testing.T) {
	cases := []Options{
		{Bodies: 1, SubSteps: 1, Gravity: 0},
		{Bodies: 8, SubSteps: 1, Gravity: 0},
		{Bodies: 8, SubSteps: 4, Gravity: 2},
		{Bodies: 37, SubSteps: 16, Gravity: -5},
	}
	for _, opt := range cases {
		w, err := New(opt)
		if err != nil {
			t.Fatal(err)
		}
		for tick := 0; tick < 30; tick++ {
			cmds := []Command{
				{Tick: tick, Body: tick % opt.Bodies, PushX: (tick*7 + 3) << 10, PushY: -(tick * 13) << 9},
				{Tick: tick, Body: (tick * 3) % opt.Bodies, PushX: 0, PushY: 1 << 14},
			}
			if err := w.Step(cmds); err != nil {
				t.Fatal(err)
			}
			full := w.Hash()
			inc := w.HashIncremental()
			if full != inc {
				t.Fatalf("opt=%+v tick=%d: 全量 %016x != 增量 %016x", opt, tick, full, inc)
			}
			if eq := w.Check(); !eq.Equal() {
				t.Fatalf("opt=%+v tick=%d: Check 不等价: %+v", opt, tick, eq)
			}
			if got := w.DirtyCount(); got < 1 || got > opt.Bodies {
				t.Fatalf("opt=%+v tick=%d: 脏槽位数 %d 越界", opt, tick, got)
			}
		}
	}
}

// TestIncrementalTracksDirtyCount 验证无变化帧不重算任何槽位。
func TestIncrementalTracksDirtyCount(t *testing.T) {
	w, _ := New(Options{Bodies: 4, SubSteps: 1, Gravity: 0})
	if err := w.Step(nil); err != nil {
		t.Fatal(err)
	}
	w.HashIncremental()
	if got := w.DirtyCount(); got != 0 {
		t.Fatalf("空步不应有脏槽位，实际 %d", got)
	}
	if err := w.Step([]Command{{Body: 2, PushX: 1}}); err != nil {
		t.Fatal(err)
	}
	inc := w.HashIncremental()
	if got := w.DirtyCount(); got != 1 {
		t.Fatalf("单实体推动应只有 1 个脏槽位，实际 %d", got)
	}
	if w.Hash() != inc {
		t.Fatal("增量与全量不一致")
	}
}
