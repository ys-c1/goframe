package orderlab

import (
	"testing"

	"goframe/api"
)

// TestCanonicalMatchesProduction 是本包可信度的根：同一份输入下，
// Canonical 路径必须与生产 api.World 逐帧逐字段相同。若失败，
// 归档里的任何对比数字都不能用来代表生产实现。
func TestCanonicalMatchesProduction(t *testing.T) {
	cfg := Config{Bodies: 4, SubSteps: 5, Gravity: 3 << 16, Drag: 8520}
	frames := demoFrames(4, 12)

	w, err := api.New(api.Options{
		Bodies: cfg.Bodies, SubSteps: cfg.SubSteps,
		Gravity: int(cfg.Gravity.Raw() >> 16), DragRaw: int32(cfg.Drag),
	})
	if err != nil {
		t.Fatal(err)
	}

	lab := Run(cfg, frames, Canonical)
	for i := range frames {
		cmds := make([]api.Command, 0, len(frames[i].Pushes))
		for _, p := range frames[i].Pushes {
			cmds = append(cmds, api.Command{Body: p.Body, PushX: int(p.DX), PushY: int(p.DY)})
		}
		if err := w.Step(cmds); err != nil {
			t.Fatal(err)
		}
		got := w.Snapshot()
		if len(got) != len(lab[i]) {
			t.Fatalf("tick %d: 长度 %d vs %d", i, len(got), len(lab[i]))
		}
		for j := range got {
			if got[j] != lab[i][j] {
				t.Fatalf("tick %d 字段 %d: 生产=%d 实验台=%d", i, j, got[j], lab[i][j])
			}
		}
	}
}

// TestSwappedDiverges 是“故意调换一次”的回归测试：反例路径必须在有限帧内
// 与规范顺序分叉。若有人把实验台的 Swapped 也改成规范写法，本测试失败。
func TestSwappedDiverges(t *testing.T) {
	cfg := Config{Bodies: 3, SubSteps: 5, Gravity: 0, Drag: 8520}
	frames := demoFrames(3, 8)
	samples := Compare(cfg, frames)
	firstDiffTick := -1
	for _, s := range samples {
		if !s.Same {
			firstDiffTick = s.Tick
			break
		}
	}
	if firstDiffTick < 0 {
		t.Fatal("调换顺序后 8 帧内未分叉，反例无效")
	}
	if firstDiffTick == 0 {
		t.Logf("首帧即分叉，首个差异: %+v", *samples[0].First)
	}
}

func demoFrames(n, ticks int) []Frame {
	frames := make([]Frame, ticks)
	for i := range frames {
		frames[i] = Frame{Pushes: []Push{
			{Body: i % n, DY: 111411},      // +1.7
			{Body: (i + 1) % n, DX: 85196}, // ≈+1.3
		}}
	}
	return frames
}
