package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"goframe/api"
	"goframe/internal/clock"
)

// labResult 是 clock_lab.json 的顶层结构。
type labResult struct {
	Schema      string           `json:"schema"`
	PeriodMS    int64            `json:"period_ms"`
	FrameRateHz int64            `json:"frame_rate_hz"`
	Slots       int              `json:"slots"`
	RollbackAt  int              `json:"rollback_at"`
	Levels      []*clock.Summary `json:"levels"`
	Bounds      labBounds        `json:"bounds"`
	WorldParity worldParity      `json:"world_parity"`
	AllPass     bool             `json:"all_pass"`
}

type labBounds struct {
	FixedLagStrictFrames string `json:"fixed_lag_strict_frames"` // 0 <= lag/Δ < 1
	ClockGapMSFormula    string `json:"clock_gap_ms_formula"`    // 0 <= m-w <= ΣR_j·Δ
	FrameGapFormula      string `json:"frame_gap_formula"`       // 同相位 = ΣR_j；计入提交滞后 < ΣR_j+1
	CumDeviationFormula  string `json:"cum_deviation_formula"`   // D_W = ΣR_j, D_fixed = 0
}

type worldParity struct {
	Frames           int       `json:"frames"`
	Identical        bool      `json:"identical"`
	BaselineLastHash string    `json:"baseline_last_hash"`
	Runs             []runInfo `json:"runs"`
}

type runInfo struct {
	Label         string `json:"label"`
	Rollback      int    `json:"rollback_frames"`
	HashCount     int    `json:"hash_count"`
	LastHash      string `json:"last_hash"`
	TickMonotonic bool   `json:"tick_monotonic"`
}

func clockLab(args []string) int {
	fs := flag.NewFlagSet("clocklab", flag.ContinueOnError)
	out := fs.String("out", "artifacts", "产物输出目录")
	slots := fs.Int("slots", 120, "每条时间轴的名义槽数")
	at := fs.Int("at", 105, "回拨发生的槽号")
	frames := fs.Int("frames", 32, "与真实 api.World 对拍的帧数")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir failed:", err)
		return 1
	}

	res := &labResult{
		Schema:      "goframe-clock-lab/v1",
		PeriodMS:    clock.DefaultPeriodMS,
		FrameRateHz: 1000 / clock.DefaultPeriodMS,
		Slots:       *slots,
		RollbackAt:  *at,
		Bounds: labBounds{
			FixedLagStrictFrames: "0 <= (m(t)-nΔ)/Δ < 1",
			ClockGapMSFormula:    "0 <= m(t)-w(t) <= (Σ_j R_j)Δ；单次回拨即 <= RΔ",
			FrameGapFormula:      "同相位 F_fixed-F_wall = Σ_{τ_j<=t} R_j；计入一帧内提交滞后有 |差| < ΣR_j+1",
			CumDeviationFormula:  "D_wall = Σ_j R_j；D_fixed = 0 < 1 帧",
		},
	}

	allPass := true
	for _, r := range clock.Levels {
		s, err := clock.Run(clock.Config{
			RollbackFrames: r,
			Slots:          *slots,
			RollbackAt:     *at,
			PeriodMS:       clock.DefaultPeriodMS,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "lab run failed:", err)
			return 1
		}
		res.Levels = append(res.Levels, s)
		allPass = allPass && s.OK
		if err := writeTimelineCSV(filepath.Join(*out, fmt.Sprintf("timeline_r%d.csv", r)), s); err != nil {
			fmt.Fprintln(os.Stderr, "write timeline failed:", err)
			return 1
		}
	}

	if err := writeCombinedTimeline(filepath.Join(*out, "timeline_all.csv"), res.Levels); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeComparisonCSV(filepath.Join(*out, "comparison.csv"), res.Levels); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeDiffCSV(filepath.Join(*out, "mono_wall_diff.csv"), res.Levels, false); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// 题目要求“差值曲线给三个点”：以最明显的 100 帧档为代表，恰好三个点。
	maxLevel := res.Levels[len(res.Levels)-1]
	if err := writeDiffCSV(filepath.Join(*out, "mono_wall_diff_3points.csv"), []*clock.Summary{maxLevel}, true); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	parity, err := checkWorldParity(*frames)
	if err != nil {
		fmt.Fprintln(os.Stderr, "world parity failed:", err)
		return 1
	}
	res.WorldParity = parity
	allPass = allPass && parity.Identical
	res.AllPass = allPass

	if err := writeJSON(filepath.Join(*out, "clock_lab.json"), res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeReportTXT(filepath.Join(*out, "clock_report.txt"), res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeComparisonMD(filepath.Join(*out, "clock_comparison.md"), res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeArtifactsIndex(filepath.Join(*out, "README.md")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Print(renderReport(res))
	if !allPass {
		fmt.Fprintln(os.Stderr, "ACCEPTANCE FAILED")
		return 1
	}
	fmt.Println("all clock-lab checks PASS; artifacts in", *out)
	return 0
}

// ---------- 真实 World 对拍 ----------

func checkWorldParity(frames int) (worldParity, error) {
	type job struct {
		label    string
		rollback int
		rb       int
	}
	jobs := []job{{"no_rollback_baseline", 0, -1}}
	for _, r := range clock.Levels {
		jobs = append(jobs, job{fmt.Sprintf("rollback_r%d", r), r, 10})
	}
	wp := worldParity{Frames: frames, Identical: true}
	var base []uint64
	for _, j := range jobs {
		hashes, monotonic, err := runHashedWorld(j.rollback, j.rb, frames)
		if err != nil {
			return wp, err
		}
		ri := runInfo{
			Label: j.label, Rollback: j.rollback, HashCount: len(hashes),
			LastHash:      fmt.Sprintf("%016x", hashes[len(hashes)-1]),
			TickMonotonic: monotonic,
		}
		wp.Runs = append(wp.Runs, ri)
		if j.rollback == 0 {
			base = hashes
			wp.BaselineLastHash = ri.LastHash
			continue
		}
		if len(hashes) != len(base) {
			wp.Identical = false
			continue
		}
		for i := range base {
			if hashes[i] != base[i] {
				wp.Identical = false
				break
			}
		}
	}
	return wp, nil
}

func runHashedWorld(rollbackR, rb, frames int) ([]uint64, bool, error) {
	w, err := api.New(api.Options{Bodies: 4, SubSteps: 2, Gravity: 1})
	if err != nil {
		return nil, false, err
	}
	wall := clock.NewWallClock(0)
	mono := clock.NewMonoClock(0)
	counter := &clock.Counter{}
	var hashes []uint64
	monotonic := true
	lastTick := 0

	for slot := 0; slot <= frames; slot++ {
		if slot > 0 {
			_ = mono.AdvanceMS(clock.DefaultPeriodMS)
			_ = wall.AdvanceMS(clock.DefaultPeriodMS)
		}
		if rollbackR > 0 && slot == rb {
			if err := wall.RollbackFrames(rollbackR, clock.DefaultPeriodMS); err != nil {
				return nil, false, err
			}
		}
		_, err := counter.Poll(mono.NowMS(), 0, clock.DefaultPeriodMS, func(frame int64) error {
			if int(frame) != w.Tick() {
				return fmt.Errorf("counter 帧号 %d 与 World.Tick %d 不一致", frame, w.Tick())
			}
			cmds := []api.Command{{Tick: int(frame), Body: int(frame) % 4, PushX: 1 << 14}}
			if err := w.Step(cmds); err != nil {
				return err
			}
			if w.Tick() <= lastTick {
				monotonic = false
			}
			lastTick = w.Tick()
			hashes = append(hashes, w.Hash())
			return nil
		})
		if err != nil {
			return nil, false, err
		}
	}
	return hashes, monotonic, nil
}

// ---------- CSV ----------

var timelineHeader = []string{
	"order", "phase", "mono_ms", "wall_ms", "gap_ms",
	"ideal_frame", "wall_frame", "fixed_frame",
	"wall_delta", "fixed_delta",
	"wall_cum_regression", "fixed_cum_regression",
	"catchup_debt_frames",
}

func writeTimelineCSV(path string, s *clock.Summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	if err := cw.Write(timelineHeader); err != nil {
		return err
	}
	for _, p := range s.Points {
		rec := []string{
			fmt.Sprint(p.Order), p.Phase, fmt.Sprint(p.MonoMS), fmt.Sprint(p.WallMS), fmt.Sprint(p.GapMS),
			fmt.Sprint(p.IdealFrame), fmt.Sprint(p.WallFrame), fmt.Sprint(p.FixedFrame),
			fmt.Sprint(p.WallDelta), fmt.Sprint(p.FixedDelta),
			fmt.Sprint(p.WallCumRegression), fmt.Sprint(p.FixedCumRegression),
			fmt.Sprint(p.CatchupDebtFrames),
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeCombinedTimeline(path string, ss []*clock.Summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	header := append([]string{"rollback_frames"}, timelineHeader...)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, s := range ss {
		for _, p := range s.Points {
			rec := []string{
				fmt.Sprint(s.RollbackFrames),
				fmt.Sprint(p.Order), p.Phase, fmt.Sprint(p.MonoMS), fmt.Sprint(p.WallMS), fmt.Sprint(p.GapMS),
				fmt.Sprint(p.IdealFrame), fmt.Sprint(p.WallFrame), fmt.Sprint(p.FixedFrame),
				fmt.Sprint(p.WallDelta), fmt.Sprint(p.FixedDelta),
				fmt.Sprint(p.WallCumRegression), fmt.Sprint(p.FixedCumRegression),
				fmt.Sprint(p.CatchupDebtFrames),
			}
			if err := cw.Write(rec); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeComparisonCSV(path string, ss []*clock.Summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	header := []string{
		"rollback_frames", "frame_before", "frame_rollback_pre", "frame_rollback_post", "frame_after",
		"backward_jump_frames", "wall_cum_deviation_frames", "fixed_cum_deviation_frames",
		"fix_below_one_frame", "catchup_debt_frames", "catchup_fast_slots",
		"clock_gap_before_ms", "clock_gap_at_ms", "clock_gap_after_ms", "clock_gap_bound_ms",
		"frame_gap_bound", "frame_gap_bound_with_lag_strict", "pass",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, s := range ss {
		rec := []string{
			fmt.Sprint(s.RollbackFrames),
			fmt.Sprint(s.FrameBefore), fmt.Sprint(s.FrameRollbackPre),
			fmt.Sprint(s.FrameRollbackPost), fmt.Sprint(s.FrameAfter),
			fmt.Sprint(s.WallMaxBackwardJump),
			fmt.Sprint(s.WallCumRegression), fmt.Sprint(s.FixedCumRegression),
			fmt.Sprintf("%v", s.FixedCumRegression < 1),
			fmt.Sprint(s.CatchupDebtFrames), fmt.Sprint(s.CatchupFastSlots),
			fmt.Sprint(s.DiffPoints[0].DiffMS), fmt.Sprint(s.DiffPoints[1].DiffMS), fmt.Sprint(s.DiffPoints[2].DiffMS),
			fmt.Sprint(s.ClockGapBoundMS),
			fmt.Sprint(s.FrameGapBound), fmt.Sprintf("<%d", s.FrameGapBoundSlack),
			fmt.Sprintf("%v", s.OK),
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeDiffCSV(path string, ss []*clock.Summary, exactThree bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	withLevel := !exactThree
	header := []string{"point", "mono_ms", "wall_ms", "diff_ms", "diff_frames"}
	if withLevel {
		header = append([]string{"rollback_frames"}, header...)
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, s := range ss {
		for _, dp := range s.DiffPoints {
			rec := []string{dp.Point, fmt.Sprint(dp.MonoMS), fmt.Sprint(dp.WallMS), fmt.Sprint(dp.DiffMS), fmt.Sprint(dp.DiffFrames)}
			if withLevel {
				rec = append([]string{fmt.Sprint(s.RollbackFrames)}, rec...)
			}
			if err := cw.Write(rec); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

// ---------- 文本报告 ----------

func renderReport(res *labResult) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(&b, "系统时钟回拨 × 帧号单调性实验报告（全部整数运算，可复算）")
	fmt.Fprintf(&b, "帧周期 Δ=%dms（%dHz 定步长）；每条时间轴 %d 槽，回拨点 rb=%d；回拨幅度 R=1/10/100 帧，每档只回拨一次。\n\n",
		res.PeriodMS, res.FrameRateHz, res.Slots, res.RollbackAt)

	fmt.Fprintln(&b, "一、三档对照：偏差与帧号变化（单位：帧 / 钟差：ms）")
	fmt.Fprintln(w, "R(帧)\tF(rb-1)\tF(rb^-)\tF(rb^+)\tF(rb+1)\t回拨瞬间增量\t墙钟D_wall\t自增D_fixed\t<1帧\t追帧欠账\t快进槽数\t钟差三点(前/拨/后,ms)")
	for _, s := range res.Levels {
		fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%v\t%d\t%d\t0/%d/%d\n",
			s.RollbackFrames, s.FrameBefore, s.FrameRollbackPre, s.FrameRollbackPost, s.FrameAfter,
			-s.WallMaxBackwardJump, s.WallCumRegression, s.FixedCumRegression, s.FixPass,
			s.CatchupDebtFrames, s.CatchupFastSlots,
			s.DiffPoints[1].DiffMS, s.DiffPoints[2].DiffMS)
	}
	w.Flush()

	fmt.Fprintln(&b, "\n二、各档回拨前后墙钟帧号序列（注意 rb^- 与 rb^+ 两个同时刻点）")
	for _, s := range res.Levels {
		seq := strings.Trim(strings.Join(strings.Fields(fmt.Sprint(s.LocalSequence)), ","), "[]")
		fmt.Fprintf(&b, "R=%3d 帧: ...,%s,...  跌幅=%d 帧；D_wall=ΣR_j=%d；D_fixed=0\n",
			s.RollbackFrames, seq, s.RollbackFrames, s.WallCumRegression)
	}

	fmt.Fprintln(&b, "\n三、单调钟 − 墙钟 差值曲线（每档三个点：回拨前 / 回拨瞬间 / 回拨后）")
	for _, s := range res.Levels {
		fmt.Fprintf(&b, "R=%3d 帧: before(t=%dms,d=0ms) -> rollback(t=%dms,d=%dms) -> after(t=%dms,d=%dms)；上界 0≤d≤%dms=RΔ\n",
			s.RollbackFrames,
			s.DiffPoints[0].MonoMS, s.DiffPoints[1].MonoMS, s.DiffPoints[1].DiffMS,
			s.DiffPoints[2].MonoMS, s.DiffPoints[2].DiffMS, s.ClockGapBoundMS)
	}

	fmt.Fprintln(&b, "\n四、上界")
	fmt.Fprintln(&b, "  钟差: 0 ≤ m(t)-w(t) ≤ (Σ_j R_j)Δ，单次回拨 ≤ RΔ（10/100/1000ms）")
	fmt.Fprintln(&b, "  帧号差(同相位): F_fixed-F_wall = 截至 t 的回拨累计 S(t)，故 ≤ ΣR_j（本实验=R）")
	fmt.Fprintln(&b, "  帧号差(计入一帧内提交滞后): |差| < ΣR_j + 1")
	fmt.Fprintln(&b, "  修复方案自身滞后: 0 ≤ m(t)-nΔ < Δ，即 lag/Δ ∈ [0,1)，严格低于一帧；累计回退 D_fixed=0")

	fmt.Fprintln(&b, "\n五、验收（三档修复后累计偏差都必须 < 1 帧）")
	for _, s := range res.Levels {
		status := "PASS"
		if !s.OK {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "  R=%3d: 墙钟非单调=%v, D_wall=%d=R, D_fixed=%d (<1) -> %s\n",
			s.RollbackFrames, s.WallProofOK, s.WallCumRegression, s.FixedCumRegression, status)
	}
	wp := res.WorldParity
	fmt.Fprintf(&b, "  真实 api.World 对拍: %d 帧 × 4 组(基线+三档回拨), 指纹完全一致=%v\n", wp.Frames, wp.Identical)
	for _, ri := range wp.Runs {
		fmt.Fprintf(&b, "    %-22s hashes=%d last=%s tick单调=%v\n", ri.Label, ri.HashCount, ri.LastHash, ri.TickMonotonic)
	}
	fmt.Fprintf(&b, "\n总验收: %v\n", map[bool]string{true: "PASS", false: "FAIL"}[res.AllPass])
	return b.String()
}

func writeReportTXT(path string, res *labResult) error {
	return os.WriteFile(path, []byte(renderReport(res)), 0o644)
}

// ---------- Markdown 对照 ----------

func writeComparisonMD(path string, res *labResult) error {
	var b strings.Builder
	fmt.Fprintln(&b, "# 三档回拨：偏差与帧号变化对照")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "帧周期 Δ=%dms（%dHz），回拨点 rb=%d，每档回拨一次。完整证明见 `docs/clock-rollback.md`。\n",
		res.PeriodMS, res.FrameRateHz, res.RollbackAt)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## 对照表")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| 回拨幅度 R(帧) | 回拨前 F(rb−1) | 回拨瞬间前 F(rb⁻) | 回拨瞬间后 F(rb⁺) | 回拨后 F(rb+1) | 最大回退量(帧) | 墙钟累计偏差 D_wall(帧)=ΣR | 自增累计偏差 D_fixed(帧) | <1 帧 | 追平快进(欠账/快进槽) | 钟差三点 前/拨/后(ms) | 结论 |")
	fmt.Fprintln(&b, "|---:|---:|---:|---:|---:|---:|---:|---:|:---:|---:|---:|:---|")
	for _, s := range res.Levels {
		verdict := "✅ PASS"
		if !s.OK {
			verdict = "❌ FAIL"
		}
		below := "是"
		if !s.FixPass {
			below = "否"
		}
		fmt.Fprintf(&b, "| %d | %d | %d | %d | %d | %d | **%d** | **%d** | %s | %d / %d | 0 / %d / %d | %s |\n",
			s.RollbackFrames, s.FrameBefore, s.FrameRollbackPre, s.FrameRollbackPost, s.FrameAfter,
			s.WallMaxBackwardJump, s.WallCumRegression, s.FixedCumRegression, below,
			s.CatchupDebtFrames, s.CatchupFastSlots,
			s.DiffPoints[1].DiffMS, s.DiffPoints[2].DiffMS, verdict)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## 各档帧号局部序列（`rb−2 … rb⁻, rb⁺, rb+1, rb+2`）")
	fmt.Fprintln(&b)
	for _, s := range res.Levels {
		// fmt.Sprint 产生 [a b c]，这里手工拼成箭头序列。
		raw := s.LocalSequence
		parts := make([]string, len(raw))
		for i, v := range raw {
			parts[i] = fmt.Sprint(v)
		}
		fmt.Fprintf(&b, "- **R=%d 帧**：`%s`；在同一单调时刻从 %d 跳到 %d，跌幅正好 %d 帧。最大幅度组的“悬崖”最明显。\n",
			s.RollbackFrames, strings.Join(parts, " → "), s.FrameRollbackPre, s.FrameRollbackPost, s.RollbackFrames)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## 单调钟与墙钟差值曲线（三个点：回拨前 / 回拨瞬间 / 回拨后）")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| R(帧) | point=before (mono,wall,diff) ms | point=rollback (mono,wall,diff) ms | point=after (mono,wall,diff) ms | 上界 |")
	fmt.Fprintln(&b, "|---:|---|---|---|---|")
	for _, s := range res.Levels {
		d0, d1, d2 := s.DiffPoints[0], s.DiffPoints[1], s.DiffPoints[2]
		fmt.Fprintf(&b, "| %d | (%d,%d,**%d**) | (%d,%d,**%d**) | (%d,%d,**%d**) | 0≤diff≤%dms=RΔ |\n",
			s.RollbackFrames,
			d0.MonoMS, d0.WallMS, d0.DiffMS,
			d1.MonoMS, d1.WallMS, d1.DiffMS,
			d2.MonoMS, d2.WallMS, d2.DiffMS,
			s.ClockGapBoundMS)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "机器可读数据：`mono_wall_diff.csv`（三档×三点）、`mono_wall_diff_3points.csv`（100 帧档恰好三点）。")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "![三档回拨对照图](clock_chart.png)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "上图由 `python3 lab/plot_clock.py artifacts` 依据 CSV 绘制（同时输出 `clock_chart.svg`）。")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## 上界")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- 钟差：$0 \\le m(t)-w(t) \\le (\\sum_j R_j)\\Delta$；单次回拨 $\\le R\\Delta$（三档分别 10 / 100 / 1000 ms）。")
	fmt.Fprintln(&b, "- 两种帧号差（同相位）：$F_{fixed}-F_{wall}=S(t)=\\sum_{\\tau_j\\le t}R_j$，故 $0\\le F_{fixed}-F_{wall}\\le\\sum_jR_j$。")
	fmt.Fprintln(&b, "- 计入一帧内提交滞后：$|F_{fixed}-F_{wall}|<\\sum_jR_j+1$。")
	fmt.Fprintln(&b, "- 修复方案自身相对单调时间的滞后：$0\\le m(t)-n\\Delta<\\Delta$，即归一化滞后 $\\in[0,1)$，**严格低于一帧**；累计回退偏差恒为 0。")
	fmt.Fprintln(&b)
	parityText := "一致 ✅"
	if !res.WorldParity.Identical {
		parityText = "不一致 ❌"
	}
	fmt.Fprintf(&b, "真实 `api.World` 对拍（%d 帧，基线 + 三档回拨共 4 组）指纹：**%s**。\n",
		res.WorldParity.Frames, parityText)
	fmt.Fprintln(&b)
	allText := "**PASS**：三档修复后累计偏差均为 0（< 1 帧），两个命题与端到端对拍全部成立。"
	if !res.AllPass {
		allText = "**FAIL**：存在未通过项，见 `clock_report.txt`。"
	}
	fmt.Fprintf(&b, "总验收：%s\n", allText)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeArtifactsIndex(path string) error {
	content := `# clock-lab 产物索引

由 ` + "`go run ./cmd/goframe clocklab`" + ` 重新生成（数值确定，无时间戳）。

- ` + "`clock_comparison.md`" + ` —— 三档幅度的偏差与帧号变化对照（主表，含图）。
- ` + "`clock_chart.png` / `clock_chart.svg`" + ` —— 上：三档帧号悬崖 vs 自增；下：钟差三点阶跃曲线。由 ` + "`python3 lab/plot_clock.py artifacts`" + ` 生成。
- ` + "`comparison.csv`" + ` —— 同一张对照表的机器可读版。
- ` + "`timeline_r1.csv` / `timeline_r10.csv` / `timeline_r100.csv`" + ` —— 三档回拨各一条完整时间轴。
- ` + "`timeline_all.csv`" + ` —— 三条时间轴合并版（多一列 rollback_frames）。
- ` + "`mono_wall_diff.csv`" + ` —— 单调钟−墙钟差值曲线，三档 × 三个点。
- ` + "`mono_wall_diff_3points.csv`" + ` —— 100 帧档（最明显组）恰好三个点：回拨前 0、回拨瞬间 1000ms、回拨后 1000ms。
- ` + "`clock_lab.json`" + ` —— 全部摘要、逐点数据、上界与 api.World 对拍结果。
- ` + "`clock_report.txt`" + ` —— 纯文本验收报告。

列含义见 ` + "`docs/clock-rollback.md`" + `。
`
	return os.WriteFile(path, []byte(content), 0o644)
}
