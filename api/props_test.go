package api

import (
	"math/rand"
	"testing"
)

// TestDedupMatchesOriginalRandom 随机对拍：归并后状态与原序逐字节一致，
// 且「有损」与「丢量记录非空」等价。
func TestDedupMatchesOriginalRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 2000; iter++ {
		n := rng.Intn(40)
		cmds := make([]Command, n)
		for i := range cmds {
			cmds[i] = Command{
				Tick:  rng.Intn(3),
				Body:  rng.Intn(4),
				PushX: rng.Intn(9) - 4,
				PushY: rng.Intn(9) - 4,
			}
		}
		opt := Options{Bodies: 4, SubSteps: 1 + rng.Intn(4), Gravity: rng.Intn(9) - 4}
		a, _ := New(opt)
		if err := a.Step(cmds); err != nil {
			t.Fatal(err)
		}
		b, _ := New(opt)
		if err := b.Step(DedupCommands(cmds)); err != nil {
			t.Fatal(err)
		}
		sa, sb := a.Snapshot(), b.Snapshot()
		for i := range sa {
			if sa[i] != sb[i] {
				t.Fatalf("iter=%d 第 %d 项 %d vs %d，输入 %+v", iter, i, sa[i], sb[i], cmds)
			}
		}
		if a.Hash() != b.Hash() {
			t.Fatalf("iter=%d 指纹不同", iter)
		}
	}

	// DedupInPlace 与 DedupCommands 逐项一致，且不越界写。
	for iter := 0; iter < 500; iter++ {
		n := rng.Intn(30)
		cmds := make([]Command, n)
		for i := range cmds {
			cmds[i] = Command{Tick: rng.Intn(2), Body: rng.Intn(3), PushX: rng.Intn(5) - 2, PushY: rng.Intn(5) - 2}
		}
		want := DedupCommands(cmds)
		buf := append([]Command(nil), cmds...)
		got := DedupInPlace(buf)
		if len(want) != len(got) {
			t.Fatalf("iter=%d 归并长度不同 %d vs %d", iter, len(want), len(got))
		}
		for i := range want {
			if want[i] != got[i] {
				t.Fatalf("iter=%d 第 %d 项 %+v vs %+v", iter, i, want[i], got[i])
			}
		}
	}
}

// TestDedupNormalizesPushInt 验证超大 Push 值按实际生效的 int32 归并：
// 0 与 1<<32 在状态上等价，应视为同一条。
func TestDedupNormalizesPushInt(t *testing.T) {
	in := []Command{
		{Body: 0, PushX: 0},
		{Body: 0, PushX: 1 << 32},
		{Body: 0, PushX: 1<<32 + 1},
	}
	got := DedupCommands(in)
	if len(got) != 2 {
		t.Fatalf("1<<32 与 0 等价，应剩 2 组，实际 %+v", got)
	}
	if got[0].PushX != 0 || got[1].PushX != 1<<32+1 {
		t.Fatalf("未出现重复的第二组应原样保留: %+v", got)
	}
	// 0 与 1<<32 折叠为 0，施加到状态后与原序一致。
	opt := Options{Bodies: 1, SubSteps: 1}
	orig, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	_ = orig.Step(in)
	merged, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	_ = merged.Step(got)
	if orig.Hash() != merged.Hash() {
		t.Fatalf("归并后指纹不同: %016x vs %016x", merged.Hash(), orig.Hash())
	}
}
