package api

import (
	"errors"
	"testing"

	"goframe/internal/snap"
)

// TestRollbackFieldByField 跑 6 帧，保存每帧前的全量快照，
// 然后逐帧 Rollback，用 snap.Compare 按字段逐个核对，
// 而不是整体比较。五帧环之外的第六帧应已被挤出。
func TestRollbackFieldByField(t *testing.T) {
	w, err := New(Options{Bodies: 8, SubSteps: 2, Gravity: 3})
	if err != nil {
		t.Fatal(err)
	}
	idx := w.FieldIndex()

	const totalFrames = 6
	before := make([][]int32, totalFrames)
	for f := 0; f < totalFrames; f++ {
		before[f] = w.Snapshot() // 该帧 Step 前的全量快照
		cmds := []Command{{Tick: f, Body: f % 8, PushX: 1 << 14, PushY: -1 << 13}}
		if err := w.Step(cmds); err != nil {
			t.Fatal(err)
		}
	}
	if w.RollbackFrames() != 5 {
		t.Fatalf("环保留帧数=%d, 应为 5", w.RollbackFrames())
	}

	// 最新一帧（第 6 帧，下标 5）先回滚，应对齐 before[5]，
	// 依次向旧，最后只能回到 before[1]；before[0] 已随最旧帧被挤出。
	for f := totalFrames - 1; f >= 1; f-- {
		if err := w.Rollback(); err != nil {
			t.Fatalf("rollback frame %d: %v", f, err)
		}
		if w.Tick() != f {
			t.Fatalf("回滚后 tick=%d, 应为 %d", w.Tick(), f)
		}
		got := w.Snapshot()
		rep, err := snap.Compare(idx, before[f], got)
		if err != nil {
			t.Fatal(err)
		}
		if !rep.AllEqual {
			t.Fatalf("回滚到帧前 %d 时逐字段比对失败: %s", f, rep.SummaryLine())
		}
		if rep.Total != idx.N() {
			t.Fatalf("比对字段数=%d, 应为 %d", rep.Total, idx.N())
		}
	}
	// 五帧已全部弹出，再回滚必须报 ErrNoRollback。
	if err := w.Rollback(); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("超出五帧后应返回 ErrNoRollback, got %v", err)
	}
}

// TestRollbackMatchesReferenceWorld 用独立参考世界给出第二道证据：
// 回滚到第 2 帧前的 w，其指纹必须与只跑前两帧的参考世界完全一致。
func TestRollbackMatchesReferenceWorld(t *testing.T) {
	opt := Options{Bodies: 4, SubSteps: 1, Gravity: 1}
	cmds := [][]Command{
		{{Tick: 0, Body: 0, PushX: 7}},
		{{Tick: 1, Body: 1, PushY: -9}},
		{{Tick: 2, Body: 2, PushX: 3, PushY: 5}},
	}
	w, _ := New(opt)
	for f := 0; f < 3; f++ {
		if err := w.Step(cmds[f]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Rollback(); err != nil { // 撤销第 3 帧
		t.Fatal(err)
	}

	ref, _ := New(opt)
	for f := 0; f < 2; f++ {
		if err := ref.Step(cmds[f]); err != nil {
			t.Fatal(err)
		}
	}
	if w.Tick() != ref.Tick() || w.Hash() != ref.Hash() {
		t.Fatalf("回滚后 tick/hash=%d/%016x 与参考世界 %d/%016x 不一致",
			w.Tick(), w.Hash(), ref.Tick(), ref.Hash())
	}
}
