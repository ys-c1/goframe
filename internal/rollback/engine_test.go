package rollback

import (
	"errors"
	"testing"

	"goframe/api"
)

func testOpts() api.Options { return api.Options{Bodies: 4, SubSteps: 2, Gravity: 1} }

func pushCmd(tick, body int) api.Command {
	return api.Command{Tick: tick, Body: body, PushX: 1 << 14, PushY: 1 << 13}
}

// TestRestoreHashEqual 验证快照保存再恢复后，哈希与原帧逐位一致。
func TestRestoreHashEqual(t *testing.T) {
	w, _ := api.New(testOpts())
	if err := w.Step([]api.Command{pushCmd(0, 0)}); err != nil {
		t.Fatal(err)
	}
	want := w.Hash()
	snap := w.SaveState()
	if err := w.Step([]api.Command{pushCmd(1, 1)}); err != nil {
		t.Fatal(err)
	}
	if w.Hash() == want {
		t.Fatal("推进后哈希不应等于旧帧")
	}
	if err := w.RestoreState(snap); err != nil {
		t.Fatal(err)
	}
	if w.Hash() != want {
		t.Fatalf("恢复后哈希 %x != 原帧 %x", w.Hash(), want)
	}
}

// TestInWindowRollbackConverges 验证窗口内迟到输入触发回滚后，
// 重放结果与"输入准时到达"的权威哈希一致。
func TestInWindowRollbackConverges(t *testing.T) {
	opts := testOpts()
	frames := 9
	// 权威序列：每帧本地指令 + 第 2 帧一条远端指令。
	trusted := buildTrusted(t, opts, frames, map[int][]api.Command{
		2: {pushCmd(2, 3)},
	})

	eng, err := New(opts, 5)
	if err != nil {
		t.Fatal(err)
	}
	eng.SetTrusted(trusted)
	// 推进 8 帧后 tick=8，环容量 6 保存 tick 2..7 的推进前快照，窗口 [2,7]。
	for tick := 0; tick < 8; tick++ {
		if err := eng.Advance([]api.Command{pushCmd(tick, tick%4)}); err != nil {
			t.Fatal(err)
		}
	}
	// 第 2 帧远端输入延迟 6 帧、此刻到达：target=2 恰在窗口最老边界，
	// 恢复到 tick=2 推进前状态后重放 6 帧（2..7），终点哈希等于 trusted[7]。
	if err := eng.Inject(8, 2, []api.Command{pushCmd(2, 3)}, 6); err != nil {
		t.Fatalf("窗口内回滚不应报错: %v", err)
	}
	if eng.Hash() != trusted[7] {
		t.Fatalf("回滚重放后哈希 %x != 权威 %x", eng.Hash(), trusted[7])
	}
	for _, ev := range eng.Events() {
		if ev.Kind == EventDiverge || ev.Kind == EventOutOfWind {
			t.Fatalf("不应出现 %s 事件: %+v", ev.Kind, ev)
		}
	}
}

// TestWindowBoundary 验证环形窗口边界帧的确切成败：
// D=5、8 次推进后（tick=8），环保存 tick 2..7 的推进前快照，
// target=2（延迟 6 帧）可回滚，target=1（延迟 7 帧）越界。
func TestWindowBoundary(t *testing.T) {
	opts := testOpts()

	// 分支一：target=2 在窗口最老边界，延迟 6 帧，必须成功且与权威一致。
	trustedOK := buildTrusted(t, opts, 9, map[int][]api.Command{
		2: {pushCmd(2, 2)},
	})
	engOK, _ := New(opts, 5)
	engOK.SetTrusted(trustedOK)
	for tick := 0; tick < 8; tick++ {
		if err := engOK.Advance([]api.Command{pushCmd(tick, tick%4)}); err != nil {
			t.Fatal(err)
		}
	}
	lo, hi := engOK.Window()
	if lo != 2 || hi != 7 {
		t.Fatalf("窗口应为 [2,7]，实际 [%d,%d]", lo, hi)
	}
	if err := engOK.Inject(8, 2, []api.Command{pushCmd(2, 2)}, 6); err != nil {
		t.Fatalf("边界帧 target=2 应可回滚: %v", err)
	}
	if engOK.Hash() != trustedOK[7] {
		t.Fatalf("边界回滚后哈希 %x != 权威 %x", engOK.Hash(), trustedOK[7])
	}
	for _, ev := range engOK.Events() {
		if ev.Kind == EventDiverge || ev.Kind == EventOutOfWind {
			t.Fatalf("成功分支不应出现 %s: %+v", ev.Kind, ev)
		}
	}

	// 分支二：target=1 已被驱逐，延迟 7 帧，必须越界。独立引擎避免输入表污染。
	engBad, _ := New(opts, 5)
	for tick := 0; tick < 8; tick++ {
		if err := engBad.Advance([]api.Command{pushCmd(tick, tick%4)}); err != nil {
			t.Fatal(err)
		}
	}
	err := engBad.Inject(8, 1, []api.Command{pushCmd(1, 2)}, 7)
	if !errors.Is(err, ErrOutOfWindow) {
		t.Fatalf("target=1 应返回 ErrOutOfWindow，实际: %v", err)
	}
	last := engBad.Events()[len(engBad.Events())-1]
	if last.Kind != EventOutOfWind || last.Target != 1 || last.WindowLo != 2 {
		t.Fatalf("越界事件字段错误: %+v", last)
	}
	if last.Note == "" {
		t.Fatal("越界事件必须写明帧号关系")
	}
}

// TestFailureTicksReproducible 验证同样的延迟轨迹跑两次，失败帧号完全一致。
func TestFailureTicksReproducible(t *testing.T) {
	opts := testOpts()
	delays := []int{0, 7, 2, 6, 1, 8, 3, 5}
	run := func() []int {
		eng, _ := New(opts, 5)
		var failed []int
		for tick := 0; tick < len(delays); tick++ {
			if err := eng.Advance([]api.Command{pushCmd(tick, tick%4)}); err != nil {
				t.Fatal(err)
			}
		}
		for target, d := range delays {
			atTick := target + d
			err := eng.Inject(atTick, target, []api.Command{pushCmd(target, 2)}, d)
			if errors.Is(err, ErrOutOfWindow) {
				failed = append(failed, target)
			}
		}
		return failed
	}
	first := run()
	second := run()
	if len(first) == 0 {
		t.Fatal("该轨迹应当产生越界失败")
	}
	if len(first) != len(second) {
		t.Fatalf("两次运行失败帧数不同: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("失败帧号不可复现: %v vs %v", first, second)
		}
	}
}

// buildTrusted 构造"所有输入准时到达"的权威哈希序列。
func buildTrusted(t *testing.T, opts api.Options, frames int, remote map[int][]api.Command) []uint64 {
	t.Helper()
	w, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	hashes := make([]uint64, 0, frames)
	for tick := 0; tick < frames; tick++ {
		cmds := []api.Command{pushCmd(tick, tick%opts.Bodies)}
		cmds = append(cmds, remote[tick]...)
		if err := w.Step(cmds); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, w.Hash())
	}
	return hashes
}
