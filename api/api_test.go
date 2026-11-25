package api

import (
	"testing"

	"goframe/internal/fixed"
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

func TestFoldImpulseMatchesRepeatedAdd(t *testing.T) {
	ds := []int{0, 1, -1, 3, 1 << 16, -1 << 15, 1<<31 - 1, -1 << 31, 1234567}
	for _, d := range ds {
		for n := 1; n <= 8; n++ {
			got := fixed.FromRaw(0)
			for i := 0; i < n; i++ {
				got = got.Add(fixed.FromRaw(d))
			}
			if want := fixed.FromRaw(foldImpulse(d, n)); got != want {
				t.Fatalf("d=%d n=%d 折叠 %v 与重复加法 %v 不符", d, n, want, got)
			}
		}
	}
}

func TestDedupKeepsRepeatedAddExact(t *testing.T) {
	cmds := []Command{{Tick: 0, Body: 0, PushX: 1 << 16}}
	both := append(append([]Command(nil), cmds...), cmds[0])

	orig, _ := New(Options{Bodies: 1, SubSteps: 1})
	if err := orig.Step(both); err != nil {
		t.Fatal(err)
	}
	if vx := orig.Snapshot()[2]; vx != 2<<16 {
		t.Fatalf("原序执行速度 %d，期望 %d", vx, 2<<16)
	}

	merged, _ := New(Options{Bodies: 1, SubSteps: 1})
	if err := merged.Step(DedupCommands(both)); err != nil {
		t.Fatal(err)
	}
	if orig.Hash() != merged.Hash() {
		t.Fatalf("归并后指纹 %016x 与原序 %016x 不同", merged.Hash(), orig.Hash())
	}
	for i, v := range orig.Snapshot() {
		if merged.Snapshot()[i] != v {
			t.Fatalf("归并后第 %d 项 %d 与原序 %d 不同", i, merged.Snapshot()[i], v)
		}
	}

	dropped, _ := New(Options{Bodies: 1, SubSteps: 1})
	if err := dropped.Step(dropDuplicates(both)); err != nil {
		t.Fatal(err)
	}
	if vx := dropped.Snapshot()[2]; vx != 1<<16 {
		t.Fatalf("丢弃基线应只有一半速度，实际 %d", vx)
	}
	if loss := DropLoss(both); len(loss) != 1 || loss[0].LostX != 1<<16 || loss[0].LostY != 0 {
		t.Fatalf("丢失冲量记录不符: %+v", loss)
	}
}

func TestDedupGroupsAndOrder(t *testing.T) {
	in := []Command{
		{Tick: 7, Body: 1, PushX: 5},
		{Tick: 7, Body: 2, PushY: -3},
		{Tick: 7, Body: 1, PushX: 5},
		{Tick: 7, Body: 2, PushY: -3},
		{Tick: 7, Body: 2, PushY: -3},
		{Tick: 8, Body: 1, PushX: 5}, // 帧号不同，不算重复
	}
	got := DedupCommands(in)
	if len(got) != 3 {
		t.Fatalf("归并后应剩 3 条，实际 %d: %+v", len(got), got)
	}
	want := []Command{
		{Tick: 7, Body: 1, PushX: 10},
		{Tick: 7, Body: 2, PushY: -9},
		{Tick: 8, Body: 1, PushX: 5},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条 %+v，期望 %+v", i, got[i], want[i])
		}
	}
	if in[0].PushX != 5 {
		t.Fatal("DedupCommands 不得修改入参")
	}
	if !DropIsLossless([]Command{{Tick: 0, Body: 3}}) {
		t.Fatal("零冲量副本丢弃应无损")
	}
	if DropIsLossless(in) {
		t.Fatal("非零重复应有损")
	}
	if len(DropLoss(in)) != 2 {
		t.Fatalf("应有两组重复，实际 %+v", DropLoss(in))
	}
}
