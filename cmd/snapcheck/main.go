// Command snapcheck 生成“全量快照 → 编号+旧值差分”改造的全部对照产物：
// 三档改动量的体积/上界/逐字段还原差异表、两种快照的分别存档、
// 最小档与最大档的上界验证，以及真实 api.World 五帧回滚的逐字段核对。
//
// 用法：
//
//	go run ./cmd/snapcheck [-out artifacts/rollback]
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goframe/api"
	"goframe/internal/sim"
	"goframe/internal/snap"
)

const bodies = 8 // N=4*8=32，与 README 示例及 DefaultOptions 一致

type tierSpec struct {
	Key    string
	Label  string
	K      int
	Mutate func(i int, v int32) int32
}

type tierReport struct {
	Key             string           `json:"key"`
	Label           string           `json:"label"`
	K               int              `json:"changed_fields"`
	N               int              `json:"total_fields"`
	FullBytes       int              `json:"full_bytes"`
	DeltaBytes      int              `json:"delta_bytes"`
	DeltaBoundBytes int              `json:"delta_bound_bytes"`
	Ratio           float64          `json:"delta_over_full_ratio"`
	RatioBoundK     float64          `json:"tier_ratio_upper_bound"`
	RatioUpperB     float64          `json:"global_ratio_upper_bound_B_N"`
	WithinBound     bool             `json:"within_global_bound"`
	BoundExact      bool             `json:"size_equals_tier_bound"`
	Diff            *snap.DiffReport `json:"field_diff"`
}

type report struct {
	Bodies                int            `json:"bodies"`
	N                     int            `json:"field_count"`
	FullSizeBytes         int            `json:"full_size_bytes"`
	FullFormula           string         `json:"full_formula"`
	DeltaFormula          string         `json:"delta_formula"`
	GlobalRatioBound      float64        `json:"global_ratio_upper_bound"`
	GlobalRatioBoundExact string         `json:"global_ratio_upper_bound_exact"`
	SavingsThresholdK     int            `json:"delta_smaller_than_full_up_to_k"`
	Tiers                 []*tierReport  `json:"tiers"`
	BoundChecks           *boundChecks   `json:"bound_verification_min_and_max"`
	AllFieldDiffsEqual    bool           `json:"all_tier_field_diffs_equal"`
	FiveFrames            []*fiveSummary `json:"five_frame_scenarios,omitempty"`
}

type boundChecks struct {
	MinK             int     `json:"min_k"`
	MinDeltaBytes    int     `json:"min_delta_bytes"`
	MinExpectedBytes int     `json:"min_expected_bytes"`
	MinRatio         float64 `json:"min_ratio"`
	MinVerified      bool    `json:"min_bound_verified"`
	MaxK             int     `json:"max_k"`
	MaxDeltaBytes    int     `json:"max_delta_bytes"`
	MaxExpectedBytes int     `json:"max_expected_bytes"`
	MaxRatio         float64 `json:"max_ratio"`
	MaxBound         float64 `json:"max_bound"`
	MaxVerified      bool    `json:"max_bound_verified_tight_equality"`
}

type fiveSummary struct {
	Scenario           string   `json:"scenario"`
	Gravity            int      `json:"gravity_q16_integer"`
	Frames             int      `json:"frames"`
	N                  int      `json:"field_count"`
	KPerFrame          []int    `json:"changed_fields_per_frame"`
	DeltaBytesPerFrame []int    `json:"delta_bytes_per_frame"`
	FullTotalBytes     int      `json:"five_full_snapshots_bytes"`
	DeltaTotalBytes    int      `json:"five_deltas_bytes"`
	SavingsPercent     float64  `json:"delta_saving_vs_full_percent"`
	WorstDeltaBytes    int      `json:"five_deltas_worst_case_bound_bytes"`
	RollbackAllEqual   bool     `json:"rollback_field_by_field_all_equal"`
	RollbackDetails    []string `json:"rollback_diff_files"`
}

func main() {
	fs := flag.NewFlagSet("snapcheck", flag.ContinueOnError)
	out := fs.String("out", filepath.Join("artifacts", "rollback"), "产物输出目录")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "snapcheck failed:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	idx, err := snap.NewIndex(bodies)
	if err != nil {
		return err
	}
	N := idx.N()

	tiers := []tierSpec{
		{Key: "min", Label: "最小改动档（1 个字段）", K: 1, Mutate: func(_ int, v int32) int32 { return v ^ 0x00000001 }},
		{Key: "mid", Label: "中等改动档（半数 16 个字段）", K: N / 2, Mutate: func(_ int, v int32) int32 { return v ^ 0x01020304 }},
		{Key: "max", Label: "最大改动档（全部 32 个字段）", K: N, Mutate: func(_ int, v int32) int32 { return v ^ (^int32(0x55555555)) }},
	}

	base := deterministicValues(N)

	rep := &report{
		Bodies:                bodies,
		N:                     N,
		FullSizeBytes:         snap.FullSize(N),
		FullFormula:           "F(N)=10+4N",
		DeltaFormula:          "D(k)=12+6k",
		GlobalRatioBound:      snap.RatioUpperBound(N),
		GlobalRatioBoundExact: "(12+6N)/(10+4N)=204/138=34/23",
		SavingsThresholdK:     snap.DeltaSmallerThanFullThreshold(N),
	}

	fullAgg := map[string]any{
		"description": "三档场景的全量快照（before=回滚目标旧状态，after=改动后状态），与 delta_snapshots.json 对照",
		"formula":     rep.FullFormula,
		"byte_size":   rep.FullSizeBytes,
		"tiers":       map[string]any{},
	}
	deltaAgg := map[string]any{
		"description": "三档场景的差分快照，每条为字段编号+旧值；应用到 after 即还原 before",
		"formula":     rep.DeltaFormula,
		"tiers":       map[string]any{},
	}
	fullTiers := fullAgg["tiers"].(map[string]any)
	deltaTiers := deltaAgg["tiers"].(map[string]any)

	allEqual := true
	var minCheck, maxCheck *tierReport

	for _, tc := range tiers {
		oldV := append([]int32(nil), base...)
		newV := append([]int32(nil), base...)
		changed := 0
		for i := 0; i < N; i++ {
			if i >= tc.K {
				break // 只让前 K 个字段发生变化，保证改动量精确等于 K
			}
			mutated := tc.Mutate(i, newV[i])
			if mutated != newV[i] {
				newV[i] = mutated
				changed++
			}
		}
		if changed != tc.K {
			return fmt.Errorf("%s: 构造改动量=%d, 期望 %d", tc.Key, changed, tc.K)
		}

		// 证明这些编号向量就是真实 sim.State 的字段顺序。
		st := sim.NewState(bodies, 0)
		if err := st.SetRaw(oldV); err != nil {
			return err
		}
		if got := st.Raw(); !int32SliceEqual(got, oldV) {
			return fmt.Errorf("%s: SetRaw/Raw 与编号表顺序不一致", tc.Key)
		}

		fullBefore, err := snap.NewSnapshot(idx, 0, oldV)
		if err != nil {
			return err
		}
		fullAfter, err := snap.NewSnapshot(idx, 1, newV)
		if err != nil {
			return err
		}
		d, err := snap.Build(idx, oldV, newV)
		if err != nil {
			return err
		}
		d.Tier = tc.Key
		d.TickBefore = 0
		if d.ChangeCount() != tc.K {
			return fmt.Errorf("%s: Build k=%d", tc.Key, d.ChangeCount())
		}

		fb, err := fullBefore.MarshalBinary()
		if err != nil {
			return err
		}
		db, err := d.MarshalBinary()
		if err != nil {
			return err
		}
		if len(fb) != snap.FullSize(N) {
			return fmt.Errorf("%s: full bytes %d != %d", tc.Key, len(fb), snap.FullSize(N))
		}
		if len(db) != snap.DeltaSize(N, tc.K) {
			return fmt.Errorf("%s: delta bytes %d != %d", tc.Key, len(db), snap.DeltaSize(N, tc.K))
		}

		restored, err := d.RestoreValues(idx, newV)
		if err != nil {
			return err
		}
		diff, err := snap.Compare(idx, oldV, restored)
		if err != nil {
			return err
		}
		if !diff.AllEqual {
			allEqual = false
		}

		tr := &tierReport{
			Key:             tc.Key,
			Label:           tc.Label,
			K:               tc.K,
			N:               N,
			FullBytes:       len(fb),
			DeltaBytes:      len(db),
			DeltaBoundBytes: snap.DeltaSizeBound(N, tc.K),
			Ratio:           float64(len(db)) / float64(len(fb)),
			RatioBoundK:     snap.RatioBoundAtK(N, tc.K),
			RatioUpperB:     snap.RatioUpperBound(N),
			WithinBound:     float64(len(db))/float64(len(fb)) <= snap.RatioUpperBound(N)+1e-12,
			BoundExact:      len(db) == snap.DeltaSizeBound(N, tc.K),
			Diff:            diff,
		}
		rep.Tiers = append(rep.Tiers, tr)
		if tc.Key == "min" {
			minCheck = tr
		}
		if tc.Key == "max" {
			maxCheck = tr
		}

		// 落盘：逐字段差异、两种快照聚合件、二进制规范编码、还原结果。
		diffPath := filepath.Join(out, "fields", tc.Key+".field-diff.json")
		if err := writeJSON(diffPath, diff); err != nil {
			return err
		}
		if err := writeBin(filepath.Join(out, "snapshots", tc.Key+".full.bin"), fb); err != nil {
			return err
		}
		if err := writeBin(filepath.Join(out, "snapshots", tc.Key+".delta.bin"), db); err != nil {
			return err
		}
		restoredSnap, err := snap.NewSnapshot(idx, 0, restored)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(out, "snapshots", tc.Key+".restored-snapshot.json"), restoredSnap); err != nil {
			return err
		}

		fullTiers[tc.Key] = map[string]any{"label": tc.Label, "before": fullBefore, "after": fullAfter}
		deltaTiers[tc.Key] = d
	}

	rep.AllFieldDiffsEqual = allEqual
	rep.BoundChecks = &boundChecks{
		MinK:             minCheck.K,
		MinDeltaBytes:    minCheck.DeltaBytes,
		MinExpectedBytes: snap.DeltaSizeBound(N, 1),
		MinRatio:         minCheck.Ratio,
		MinVerified:      minCheck.BoundExact && minCheck.WithinBound,
		MaxK:             maxCheck.K,
		MaxDeltaBytes:    maxCheck.DeltaBytes,
		MaxExpectedBytes: snap.DeltaSizeBound(N, N),
		MaxRatio:         maxCheck.Ratio,
		MaxBound:         snap.RatioUpperBound(N),
		MaxVerified: maxCheck.BoundExact && maxCheck.WithinBound &&
			absFloat(maxCheck.Ratio-snap.RatioUpperBound(N)) < 1e-12,
	}
	if !rep.BoundChecks.MinVerified || !rep.BoundChecks.MaxVerified {
		return fmt.Errorf("上界验证失败: %+v", rep.BoundChecks)
	}
	if !allEqual {
		return fmt.Errorf("至少一档逐字段还原不一致")
	}

	if err := writeJSON(filepath.Join(out, "snapshots", "full_snapshots.json"), fullAgg); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "snapshots", "delta_snapshots.json"), deltaAgg); err != nil {
		return err
	}

	// 真实 api.World 的五帧差分环端到端对照：
	// 一个默认零重力稀疏负载（与 cmd/goframe 默认参数同风格），
	// 一个全实体重力负载（改动面大，会越过“差分<全量”的阈值）。
	for _, sc := range []struct {
		name    string
		gravity int
	}{
		{"sparse_gravity0", 0},
		{"gravity_all_bodies2", 2},
	} {
		five, err := runScenario(out, idx, sc.name, sc.gravity)
		if err != nil {
			return err
		}
		rep.FiveFrames = append(rep.FiveFrames, five)
	}
	agg := map[string]any{"scenarios": rep.FiveFrames}
	if err := writeJSON(filepath.Join(out, "five_frames", "summary.json"), agg); err != nil {
		return err
	}

	if err := writeJSON(filepath.Join(out, "report.json"), rep); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "comparison.csv"), rep); err != nil {
		return err
	}
	if err := writeMarkdown(filepath.Join(out, "comparison.md"), rep); err != nil {
		return err
	}

	printConsole(rep)
	return nil
}

// runScenario 用与 cmd/goframe 同风格的确定性指令跑 5 帧，
// 每帧另存全量与差分，然后通过 World.Rollback 逐帧回滚并逐字段核对。
func runScenario(out string, idx *snap.Index, name string, gravity int) (*fiveSummary, error) {
	const nFrames = 5
	dir := filepath.Join(out, "five_frames", name)
	w, err := api.New(api.Options{Bodies: bodies, SubSteps: 1, Gravity: gravity})
	if err != nil {
		return nil, err
	}
	type saved struct {
		tick int
		full *snap.Snapshot
		d    *snap.Delta
		fb   []byte
		db   []byte
	}
	frames := make([]saved, nFrames)

	for j := 0; j < nFrames; j++ {
		before := w.Snapshot()
		full, err := snap.NewSnapshot(idx, w.Tick(), before)
		if err != nil {
			return nil, err
		}
		cmd := []api.Command{{Tick: w.Tick(), Body: j % bodies, PushX: 1 << 14}}
		if err := w.Step(cmd); err != nil {
			return nil, err
		}
		after := w.Snapshot()
		d, err := snap.Build(idx, before, after)
		if err != nil {
			return nil, err
		}
		d.TickBefore = j
		fb, err := full.MarshalBinary()
		if err != nil {
			return nil, err
		}
		db, err := d.MarshalBinary()
		if err != nil {
			return nil, err
		}
		frames[j] = saved{tick: j, full: full, d: d, fb: fb, db: db}

		base := fmt.Sprintf("frame_%02d", j)
		if err := writeJSON(filepath.Join(dir, "full", base+".json"), full); err != nil {
			return nil, err
		}
		if err := writeBin(filepath.Join(dir, "full", base+".bin"), fb); err != nil {
			return nil, err
		}
		if err := writeJSON(filepath.Join(dir, "delta", base+".json"), d); err != nil {
			return nil, err
		}
		if err := writeBin(filepath.Join(dir, "delta", base+".bin"), db); err != nil {
			return nil, err
		}
	}

	sum := &fiveSummary{Scenario: name, Gravity: gravity, Frames: nFrames, N: idx.N(), RollbackAllEqual: true}
	for j := 0; j < nFrames; j++ {
		sum.KPerFrame = append(sum.KPerFrame, frames[j].d.ChangeCount())
		sum.DeltaBytesPerFrame = append(sum.DeltaBytesPerFrame, len(frames[j].db))
		sum.FullTotalBytes += len(frames[j].fb)
		sum.DeltaTotalBytes += len(frames[j].db)
	}
	sum.SavingsPercent = 100 * float64(sum.FullTotalBytes-sum.DeltaTotalBytes) / float64(sum.FullTotalBytes)
	sum.WorstDeltaBytes = nFrames * snap.DeltaSizeBound(idx.N(), idx.N())

	// 现在世界在 tick=5；从最新帧开始回滚，依次应对齐 before[4]..before[0]。
	for j := nFrames - 1; j >= 0; j-- {
		if err := w.Rollback(); err != nil {
			return nil, err
		}
		got := w.Snapshot()
		diff, err := snap.Compare(idx, frames[j].full.Values, got)
		if err != nil {
			return nil, err
		}
		fname := fmt.Sprintf("rollback_to_before_frame_%02d.field-diff.json", j)
		if err := writeJSON(filepath.Join(dir, "rollback", fname), diff); err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(out, filepath.Join(dir, "rollback", fname))
		if err != nil {
			return nil, err
		}
		sum.RollbackDetails = append(sum.RollbackDetails, rel)
		if w.Tick() != j || !diff.AllEqual {
			sum.RollbackAllEqual = false
			return nil, fmt.Errorf("[%s] 回滚到 before[%d] 失败: tick=%d %s", name, j, w.Tick(), diff.SummaryLine())
		}
	}
	if w.CanRollback() {
		return nil, fmt.Errorf("[%s] 五帧全部回滚后环应为空", name)
	}

	if err := writeJSON(filepath.Join(dir, "summary.json"), sum); err != nil {
		return nil, err
	}
	if err := writeScenarioMarkdown(filepath.Join(dir, "summary.md"), sum); err != nil {
		return nil, err
	}
	return sum, nil
}

func writeCSV(path string, rep *report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	header := []string{
		"档位", "改动量k", "总字段N", "全量体积F(B)", "差分实测D(B)",
		"差分上界Dbar(k)(B)", "D/F", "本档比值上界", "统一比值上界B(N)",
		"是否在界内", "体积是否取到本档上界", "逐字段还原差异",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, t := range rep.Tiers {
		row := []string{
			t.Label,
			fmt.Sprintf("%d", t.K),
			fmt.Sprintf("%d", t.N),
			fmt.Sprintf("%d", t.FullBytes),
			fmt.Sprintf("%d", t.DeltaBytes),
			fmt.Sprintf("%d", t.DeltaBoundBytes),
			fmt.Sprintf("%.6f", t.Ratio),
			fmt.Sprintf("%.6f", t.RatioBoundK),
			fmt.Sprintf("%.6f", t.RatioUpperB),
			passCN(t.WithinBound),
			passCN(t.BoundExact),
			t.Diff.SummaryLine(),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeMarkdown(path string, rep *report) error {
	var b strings.Builder
	fmt.Fprintln(&b, "# 三档改动量：差分体积、上界与逐字段还原差异")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- 字段规模：bodies=%d，N=4×%d=%d 个标量字段\n", rep.Bodies, rep.Bodies, rep.N)
	fmt.Fprintf(&b, "- 全量快照：`%s` = **%d B**\n", rep.FullFormula, rep.FullSizeBytes)
	fmt.Fprintf(&b, "- 差分快照：`%s`（编号 uint16 2B + 旧值 int32 4B = 6B/字段，头 12B）\n", rep.DeltaFormula)
	fmt.Fprintf(&b, "- 统一比值上界：B(N)=%s ≈ **%.6f**（%.2f%%）；k=N 时取到，是紧上界\n",
		rep.GlobalRatioBoundExact, rep.GlobalRatioBound, 100*rep.GlobalRatioBound)
	fmt.Fprintf(&b, "- 差分严格小于全量的改动量区间：k≤%d（N=%d）\n", rep.SavingsThresholdK, rep.N)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| 档位 | 改动量 k | 差分实测体积 | 本档上界 D̄(k)=12+6k | D/F | 统一上界 B(N)≈1.478261 | 逐字段还原差异 |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---|")
	for _, t := range rep.Tiers {
		fmt.Fprintf(&b, "| %s | %d/%d | %d B | %d B（%s） | %.4f（%.2f%%） | %.6f，%s | %s |\n",
			t.Label, t.K, t.N, t.DeltaBytes, t.DeltaBoundBytes, passCN(t.BoundExact),
			t.Ratio, 100*t.Ratio, t.RatioUpperB, passCN(t.WithinBound), t.Diff.SummaryLine())
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## 上界在最小、最大两档的验证")
	fmt.Fprintln(&b)
	bc := rep.BoundChecks
	fmt.Fprintf(&b, "- 最小档 k=%d：D=%d B，期望上界 %d B；D/F=%.6f ≤ B(N)=%.6f → **%s**\n",
		bc.MinK, bc.MinDeltaBytes, bc.MinExpectedBytes, bc.MinRatio, rep.GlobalRatioBound, verifyCN(bc.MinVerified))
	fmt.Fprintf(&b, "- 最大档 k=%d：D=%d B，期望上界 %d B；D/F=%.6f = B(N)=%.6f（紧等号）→ **%s**\n",
		bc.MaxK, bc.MaxDeltaBytes, bc.MaxExpectedBytes, bc.MaxRatio, bc.MaxBound, verifyCN(bc.MaxVerified))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## 还原比较方式")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "每个档位都对全部 32 个字段逐个生成 `FieldDiff{id,name,full_value,restored_value,equal}`，")
	fmt.Fprintln(&b, "见 `fields/<tier>.field-diff.json`；不使用整体相等比较。三档均为 32/32 一致。")
	fmt.Fprintln(&b)
	if len(rep.FiveFrames) > 0 {
		fmt.Fprintln(&b, "## 五帧快照对照（真实 api.World 回滚环，两个负载场景）")
		fmt.Fprintln(&b)
		for _, f := range rep.FiveFrames {
			fmt.Fprintf(&b, "### 场景 `%s`（gravity=%d）\n\n", f.Scenario, f.Gravity)
			fmt.Fprintf(&b, "- 每帧改动量 k=%v\n", f.KPerFrame)
			fmt.Fprintf(&b, "- 每帧差分体积 B=%v\n", f.DeltaBytesPerFrame)
			fmt.Fprintf(&b, "- 五份全量：%d B；五份差分实际合计：%d B（节省 %.1f%%）；五份差分最坏上界合计：%d B\n",
				f.FullTotalBytes, f.DeltaTotalBytes, f.SavingsPercent, f.WorstDeltaBytes)
			fmt.Fprintf(&b, "- 连续 Rollback 5 次，每次与对应全量快照逐字段比对：**%s**（明细见 `five_frames/%s/rollback/`）\n\n",
				verifyCN(f.RollbackAllEqual), f.Scenario)
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## 快照存档")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- 全量快照汇总：[`snapshots/full_snapshots.json`](snapshots/full_snapshots.json)")
	fmt.Fprintln(&b, "- 差分快照汇总：[`snapshots/delta_snapshots.json`](snapshots/delta_snapshots.json)")
	fmt.Fprintln(&b, "- 每档规范二进制：`snapshots/<tier>.full.bin` / `<tier>.delta.bin`（表中体积即其字节数）")
	fmt.Fprintln(&b, "- 每档还原结果：`snapshots/<tier>.restored-snapshot.json`")
	fmt.Fprintln(&b, "- 五帧两个负载场景分别存档：`five_frames/<scenario>/{full,delta,rollback}/`")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeScenarioMarkdown(path string, s *fiveSummary) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# 五帧对照 · 场景 `%s`（gravity=%d）\n\n", s.Scenario, s.Gravity)
	fmt.Fprintln(&b, "| 帧 | 改动字段 k | 差分体积 (B) | 全量体积 (B) |")
	fmt.Fprintln(&b, "|---:|---:|---:|---:|")
	for i, k := range s.KPerFrame {
		fmt.Fprintf(&b, "| %d | %d/%d | %d | %d |\n", i, k, s.N, s.DeltaBytesPerFrame[i], snap.FullSize(s.N))
	}
	fmt.Fprintf(&b, "| **合计** |  | **%d** | **%d** |\n\n", s.DeltaTotalBytes, s.FullTotalBytes)
	fmt.Fprintf(&b, "差分相对五份全量节省 **%.1f%%**。\n\n", s.SavingsPercent)
	fmt.Fprintf(&b, "五份差分的最坏上界合计为 5×%d=%d B（所有字段每帧都变）；实际合计 %d B。\n\n",
		snap.DeltaSizeBound(s.N, s.N), s.WorstDeltaBytes, s.DeltaTotalBytes)
	fmt.Fprintf(&b, "五次回滚逐字段比对全部相等：**%s**。\n", verifyCN(s.RollbackAllEqual))
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func printConsole(rep *report) {
	fmt.Println("档位 | k | 差分B | 上界B | D/F | 上界B(N) | 逐字段差异")
	for _, t := range rep.Tiers {
		fmt.Printf("%s | %d | %d | %d | %.4f | %.6f | %s\n",
			t.Key, t.K, t.DeltaBytes, t.DeltaBoundBytes, t.Ratio, t.RatioUpperB, t.Diff.SummaryLine())
	}
	bc := rep.BoundChecks
	fmt.Printf("验界: min(k=1) %s (%d==%d B); max(k=%d) %s (%d B, ratio %.6f == B %.6f)\n",
		verifyCN(bc.MinVerified), bc.MinDeltaBytes, bc.MinExpectedBytes,
		bc.MaxK, verifyCN(bc.MaxVerified), bc.MaxDeltaBytes, bc.MaxRatio, bc.MaxBound)
	for _, f := range rep.FiveFrames {
		fmt.Printf("五帧[%s] 回滚逐字段一致: %s; full=%dB delta=%dB (节省 %.1f%%) worst=%dB k=%v\n",
			f.Scenario, verifyCN(f.RollbackAllEqual),
			f.FullTotalBytes, f.DeltaTotalBytes, f.SavingsPercent, f.WorstDeltaBytes, f.KPerFrame)
	}
}

// deterministicValues 用 FNV 混合生成覆盖正负数域的确定性初值。
func deterministicValues(n int) []int32 {
	const (
		offset32 = uint32(2166136261)
		prime32  = uint32(16777619)
	)
	v := offset32
	out := make([]int32, n)
	for i := range out {
		v ^= uint32(i + 1)
		v *= prime32
		// 再混合一次高位，避免小下标时值过于接近。
		v ^= v >> 13
		v *= prime32
		out[i] = int32(v)
	}
	return out
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeBin(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func int32SliceEqual(a, b []int32) bool {
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

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func passCN(ok bool) string {
	if ok {
		return "是"
	}
	return "否"
}

func verifyCN(ok bool) string {
	if ok {
		return "通过 ✓"
	}
	return "失败 ✗"
}
