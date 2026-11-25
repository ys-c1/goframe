// Command goorder 归档“定点乘法不结合”的取证数据：三组数值对照表、
// 规范顺序与调换顺序的逐帧轨迹、以及首次分叉点。
//
// 输出目录默认 artifacts/mul-order：输出不含时间戳，可反复生成并逐字节比对。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goframe/internal/fixed"
	"goframe/internal/orderlab"
)

func main() {
	out := flag.String("out", "artifacts/mul-order", "归档输出目录")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "goorder:", err)
		os.Exit(1)
	}
}

type groupRow struct {
	Name       string `json:"name"`
	SubSteps   int    `json:"substeps"`
	DtRaw      int32  `json:"dt_raw"`
	VRaw       int32  `json:"v_raw"`
	KRaw       int32  `json:"k_raw"`
	Canonical  int32  `json:"canonical_raw"`
	Swapped    int32  `json:"swapped_raw"`
	Delta      int32  `json:"delta_raw"`
	Bound      int32  `json:"bound_raw"`
	BoundTight bool   `json:"bound_tight"`
	Convention string `json:"convention"`
}

type report struct {
	GeneratedAt string      `json:"generated_at_utc"`
	Convention  string      `json:"convention"`
	Groups      []groupRow  `json:"groups"`
	Trace       traceReport `json:"trace"`
	Note        string      `json:"note"`
}

type traceReport struct {
	Config     traceConfig            `json:"config"`
	Ticks      int                    `json:"ticks"`
	Frames     []orderlab.FrameSample `json:"frames"`
	FirstSplit int                    `json:"first_split_tick"`
}

type traceConfig struct {
	Bodies   int    `json:"bodies"`
	SubSteps int    `json:"substeps"`
	Gravity  int32  `json:"gravity_raw"`
	Drag     int32  `json:"drag_raw"`
	DragNote string `json:"drag_note"`
}

func run(outDir string) error {
	rows, err := buildGroups()
	if err != nil {
		return err
	}
	cfg := orderlab.Config{Bodies: 4, SubSteps: 5, Gravity: 3 << 16, Drag: 8520}
	frames := archiveFrames(cfg.Bodies, 16)
	samples := orderlab.Compare(cfg, frames)
	first := -1
	for _, s := range samples {
		if !s.Same {
			first = s.Tick
			break
		}
	}
	rep := report{
		Convention: "v*(k*dt)：右结合、系数对先行；禁止 (v*k)*dt",
		Groups:     rows,
		Trace: traceReport{
			Config: traceConfig{
				Bodies: cfg.Bodies, SubSteps: cfg.SubSteps,
				Gravity: cfg.Gravity.Raw(), Drag: cfg.Drag.Raw(),
				DragNote: "0.13 的 Q16.16 原始值",
			},
			Ticks: len(frames), Frames: samples, FirstSplit: first,
		},
		Note: "canonical 路径与 api.World 逐帧对拍，见 internal/orderlab/orderlab_test.go",
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "groups.json"), rows); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "trace.json"), rep); err != nil {
		return err
	}
	if err := writeTraceMarkdown(filepath.Join(outDir, "TRACE.md"), rep); err != nil {
		return err
	}
	return writeMarkdown(filepath.Join(outDir, "REPORT.md"), rep)
}

func buildGroups() ([]groupRow, error) {
	type spec struct {
		name        string
		sub         int
		v, k        int32
		wantCanon   int32
		wantSwapped int32
		wantBound   int32
	}
	specs := []spec{
		{"G1 v=3.3 k=0.13 sub=3", 3, 216268, 8520, 9368, 9371, 4},
		{"G2 v=5.3 k=0.13 sub=5", 5, 347341, 8520, 9025, 9031, 6},
		{"G3 v=-2.3 k=0.37 sub=7", 7, -150733, 24248, -7965, -7968, 3},
	}
	out := make([]groupRow, 0, len(specs))
	for _, s := range specs {
		dt := fixed.One.Div(fixed.FromInt(s.sub))
		v, k := fixed.FromRaw(int(s.v)), fixed.FromRaw(int(s.k))
		canon := fixed.Mul3(v, k, dt)
		swapped := v.Mul(k).Mul(dt)
		bound := fixed.Mul3Bound(v, dt)
		if canon.Raw() != s.wantCanon || swapped.Raw() != s.wantSwapped || bound.Raw() != s.wantBound {
			return nil, fmt.Errorf("组 %s 与硬编码期望不符: %d/%d/%d",
				s.name, canon.Raw(), swapped.Raw(), bound.Raw())
		}
		out = append(out, groupRow{
			Name: s.name, SubSteps: s.sub, DtRaw: dt.Raw(), VRaw: s.v, KRaw: s.k,
			Canonical: canon.Raw(), Swapped: swapped.Raw(),
			Delta: canon.Raw() - swapped.Raw(), Bound: bound.Raw(),
			BoundTight: abs32(canon.Raw()-swapped.Raw()) == bound.Raw(),
			Convention: "v*(k*dt)",
		})
	}
	return out, nil
}

func archiveFrames(n, ticks int) []orderlab.Frame {
	frames := make([]orderlab.Frame, ticks)
	for i := range frames {
		frames[i] = orderlab.Frame{Pushes: []orderlab.Push{
			{Body: i % n, DY: 111411},
			{Body: (i + 1) % n, DX: 85196},
		}}
	}
	return frames
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeMarkdown(path string, rep report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# 定点乘法不结合：三组数值与轨迹档案\n\n")
	fmt.Fprintf(f, "由 `go run ./cmd/goorder -out artifacts/mul-order` 生成，内容与时间无关、可逐字节重建。\n\n")
	fmt.Fprintf(f, "顺序约定：%s\n\n", rep.Convention)
	fmt.Fprintf(f, "| 组 | 子步 | dt | v | k | 规范顺序 v*(k*dt) | 调换顺序 (v*k)*dt | 差值 | 界 | 界取等 |\n")
	fmt.Fprintf(f, "|---|---|---|---|---|---|---|---|---|---|\n")
	for _, g := range rep.Groups {
		tight := ""
		if g.BoundTight {
			tight = "是"
		}
		fmt.Fprintf(f, "| %s | %d | %d | %d | %d | %d | %d | %+d | %d | %s |\n",
			g.Name, g.SubSteps, g.DtRaw, g.VRaw, g.KRaw,
			g.Canonical, g.Swapped, g.Delta, g.Bound, tight)
	}
	fmt.Fprintf(f, "\n## 轨迹对照（规范 vs 调换）\n\n")
	fmt.Fprintf(f, "参数：bodies=%d substeps=%d gravity_raw=%d drag_raw=%d（%s）；共 %d 帧。\n\n",
		rep.Trace.Config.Bodies, rep.Trace.Config.SubSteps,
		rep.Trace.Config.Gravity, rep.Trace.Config.Drag, rep.Trace.Config.DragNote,
		rep.Trace.Ticks)
	fmt.Fprintf(f, "首次分叉帧：%d\n\n", rep.Trace.FirstSplit)
	fmt.Fprintf(f, "| 帧 | 规范指纹 | 调换指纹 | 相同 | 首个差异 |\n|---|---|---|---|---|\n")
	for _, s := range rep.Trace.Frames {
		d := "—"
		if s.First != nil {
			d = fmt.Sprintf("body %d %s: %d → %d（%+d）",
				s.First.Body, s.First.Field, s.First.Canonical, s.First.Swapped, s.First.Delta)
		}
		fmt.Fprintf(f, "| %d | %016x | %016x | %v | %s |\n",
			s.Tick, s.CanonHash, s.SwapHash, s.Same, d)
	}
	return nil
}

// writeTraceMarkdown 只输出两轨轨迹，作为 REPORT.md 的纯轨迹副本存档。
func writeTraceMarkdown(path string, rep report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# 轨迹对照存档（规范顺序 vs 故意调换）\n\n")
	fmt.Fprintf(f, "由 `make archive`（`go run ./cmd/goorder -out artifacts/mul-order`）生成，可逐字节重建。\n\n")
	c := rep.Trace.Config
	fmt.Fprintf(f, "参数：bodies=%d substeps=%d gravity_raw=%d drag_raw=%d（%s）；共 %d 帧。\n\n",
		c.Bodies, c.SubSteps, c.Gravity, c.Drag, c.DragNote, rep.Trace.Ticks)
	fmt.Fprintf(f, "首次分叉帧：%d\n\n", rep.Trace.FirstSplit)
	fmt.Fprintf(f, "| 帧 | 规范指纹 | 调换指纹 | 相同 | 首个差异 |\n|---|---|---|---|---|\n")
	for _, s := range rep.Trace.Frames {
		d := "—"
		if s.First != nil {
			d = fmt.Sprintf("body %d %s: %d → %d（%+d）",
				s.First.Body, s.First.Field, s.First.Canonical, s.First.Swapped, s.First.Delta)
		}
		fmt.Fprintf(f, "| %d | %016x | %016x | %v | %s |\n",
			s.Tick, s.CanonHash, s.SwapHash, s.Same, d)
	}
	return nil
}

func abs32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
