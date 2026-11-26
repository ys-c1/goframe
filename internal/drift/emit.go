package drift

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FormulaSummary 是写进 JSON/报告头的公式一句话。
const FormulaSummary = "Δx_b(f,N)=e_{4b}+f·S·e_{4b+2}, Δy_b=e_{4b+1}+f·S·e_{4b+3}, " +
	"e_k=bswap32(c_{N-1-k})−c_k, N=4B; 标定图案下 Δx_b=f·N, Σ_b=fN²/4"

// Report 是 drift-report.json 的顶层结构。
type Report struct {
	FormulaSummary string          `json:"formula_summary"`
	Cases          []*CaseResult   `json:"cases"`
	CarrierGrid    []GridRow       `json:"carrier_grid"`
	LatentTable    []VisibilityRow `json:"latent_table"`
	LatentSnapshot *LatentResult   `json:"latent_snapshot"`
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func ptrInt(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

func boolStr(b bool) string { return strconv.FormatBool(b) }

// Generate 跑全部实验并把快照/表格/JSON 写到 outDir。
func Generate(outDir string) (*Report, error) {
	if err := os.MkdirAll(filepath.Join(outDir, "snapshots"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "frames"), 0o755); err != nil {
		return nil, err
	}

	rep := &Report{FormulaSummary: FormulaSummary}

	specs := DefaultSpecs()
	for i := range specs {
		cr, err := RunCase(specs[i])
		if err != nil {
			return nil, fmt.Errorf("run %s: %w", specs[i].ID, err)
		}
		cr.LegacyFile = fmt.Sprintf("snapshots/%s.legacy-v1.old-order-be.bin", cr.Spec.ID)
		cr.CanonicalFile = fmt.Sprintf("snapshots/%s.canonical-v2.canonical-order-le.bin", cr.Spec.ID)
		if err := writeFile(filepath.Join(outDir, cr.LegacyFile), cr.LegacyBytes); err != nil {
			return nil, err
		}
		if err := writeFile(filepath.Join(outDir, cr.CanonicalFile), cr.CanonicalBytes); err != nil {
			return nil, err
		}
		cr.LegacySHA256 = shaHex(cr.LegacyBytes)
		cr.CanonicalSHA = shaHex(cr.CanonicalBytes)

		if err := writeFrameCSV(outDir, cr); err != nil {
			return nil, err
		}
		if err := writeFactorialCSV(outDir, cr); err != nil {
			return nil, err
		}
		rep.Cases = append(rep.Cases, cr)
	}

	// 两种序列化各在 artifacts 根目录存一份具名样例（内容=case2）。
	case2 := rep.Cases[1]
	if err := writeFile(filepath.Join(outDir, "old-order-snapshot.v1.bin"), case2.LegacyBytes); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(outDir, "canonical-order-snapshot.v2.bin"), case2.CanonicalBytes); err != nil {
		return nil, err
	}

	// 延迟可见漂移样例。
	lat, err := RunLatent()
	if err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(outDir, lat.LegacyFile), lat.LegacyBytes); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(outDir, lat.CanonicalFile), lat.CanonicalBytes); err != nil {
		return nil, err
	}
	lat.LegacySHA256 = shaHex(lat.LegacyBytes)
	lat.CanonicalSHA256 = shaHex(lat.CanonicalBytes)
	if err := writeLatentCSV(outDir, lat); err != nil {
		return nil, err
	}
	rep.LatentSnapshot = lat
	rep.LatentTable = VisibilityTable()

	// 标定公式网格。
	grid, err := BuildCalibrationGrid()
	if err != nil {
		return nil, err
	}
	rep.CarrierGrid = grid
	if err := writeGridCSV(outDir, grid); err != nil {
		return nil, err
	}

	if err := writeSummaryCSV(outDir, rep.Cases); err != nil {
		return nil, err
	}
	if err := writeFrameComparisonMD(outDir, rep.Cases); err != nil {
		return nil, err
	}
	if err := writeJSON(outDir, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

func writeCSV(path string, header []string, rows [][]string) error {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	if err := w.Write(header); err != nil {
		return err
	}
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return writeFile(path, []byte(sb.String()))
}

func writeFrameCSV(outDir string, cr *CaseResult) error {
	header := []string{
		"f_after_checkpoint", "global_tick", "direct_hash",
		"naive_old_hash_before_normalize", "normalized_hash", "canonical_v2_file_hash",
		"naive_hash_equal_direct", "normalized_hash_equal_direct",
		"dx_body0_raw", "dy_body0_raw", "pos_max_abs_raw", "pos_max_abs_units",
		"pred_dx_body0_raw", "pred_dy_body0_raw",
		"formula_max_abs_residual", "formula_exact",
	}
	rows := make([][]string, 0, len(cr.Frames))
	for _, r := range cr.Frames {
		rows = append(rows, []string{
			strconv.Itoa(r.F), strconv.Itoa(r.Tick), r.DirectHash,
			r.NaiveHash, r.NormalizedHash, r.CanonicalFileHash,
			boolStr(r.NaiveHashEqual), boolStr(r.NormalizedHashEqual),
			strconv.FormatInt(r.Dx0Raw, 10), strconv.FormatInt(r.Dy0Raw, 10),
			strconv.FormatInt(r.PosMaxAbsRaw, 10), fmt.Sprintf("%.9f", r.PosMaxAbsUnits),
			strconv.FormatInt(r.PredDx0Raw, 10), strconv.FormatInt(r.PredDy0Raw, 10),
			strconv.FormatInt(r.FormulaMaxAbsResidual, 10), boolStr(r.FormulaExact),
		})
	}
	return writeCSV(filepath.Join(outDir, "frames", cr.Spec.ID+".frame-by-frame.csv"), header, rows)
}

func writeFactorialCSV(outDir string, cr *CaseResult) error {
	header := []string{
		"case_id", "mode", "endian_state_block", "order_state_block",
		"recovers_exact_state", "first_diff_f", "first_diff_tick",
		"deviation_at_load_raw", "deviation_at_end_raw", "final_hash",
		"all_hashes_equal_direct",
	}
	rows := make([][]string, 0, len(cr.Factorial))
	for _, r := range cr.Factorial {
		rows = append(rows, []string{
			cr.Spec.ID, r.Mode, r.Endian, r.Order, boolStr(r.RecoversExactState),
			ptrInt(r.FirstDiffF), ptrInt(r.FirstDiffTick),
			strconv.FormatInt(r.DevAtLoadRaw, 10), strconv.FormatInt(r.DevAtEndRaw, 10),
			r.FinalHash, boolStr(r.AllHashesEqualDirect),
		})
	}
	return writeCSV(filepath.Join(outDir, "factorial-"+cr.Spec.ID+".csv"), header, rows)
}

func writeSummaryCSV(outDir string, cs []*CaseResult) error {
	header := []string{
		"case_id", "label", "bodies", "field_count_N", "checkpoint_frame_C",
		"drift_start_tick_incl_checkpoint", "first_stepped_divergent_tick",
		"normalized_drift_start_tick",
		"deviation_at_start_raw", "deviation_at_start_units",
		"deviation_at_first_step_raw", "deviation_at_end_raw", "deviation_at_end_units",
		"first_whole_unit_diff_f", "first_whole_unit_diff_tick",
		"direct_hash_at_C", "before_normalize_naive_hash_at_C", "after_normalize_hash_at_C",
		"direct_final_hash", "naive_final_hash", "normalized_final_hash",
		"normalized_all_frames_match", "formula_exact", "formula_max_abs_residual",
		"legacy_file", "canonical_file", "legacy_sha256", "canonical_sha256",
	}
	rows := make([][]string, 0, len(cs))
	for _, c := range cs {
		normAll := c.NormalizedDriftStartTick == nil
		rows = append(rows, []string{
			c.Spec.ID, c.Spec.Label, strconv.Itoa(c.Spec.Bodies), strconv.Itoa(c.SpecN),
			strconv.Itoa(c.Spec.Checkpoint),
			ptrInt(c.DriftStartTickInclCheckpoint), ptrInt(c.FirstSteppedDivergentTick),
			ptrInt(c.NormalizedDriftStartTick),
			strconv.FormatInt(c.DeviationAtDriftStartRaw, 10), fmt.Sprintf("%.6f", c.DeviationAtDriftStartUnit),
			strconv.FormatInt(c.DeviationAtFirstStepRaw, 10),
			strconv.FormatInt(c.DeviationAtEndRaw, 10), fmt.Sprintf("%.6f", c.DeviationAtEndUnits),
			ptrInt(c.FirstWholeUnitDiffF), ptrInt(c.FirstWholeUnitDiffTick),
			c.CheckpointHashDirect, c.NaiveCheckpointHash, c.NormalizedCheckpointHash,
			c.DirectFinalHash, c.NaiveFinalHash, c.NormalizedFinalHash,
			boolStr(normAll), boolStr(c.FormulaExactAllFrames),
			strconv.FormatInt(c.FormulaMaxAbsResidual, 10),
			c.LegacyFile, c.CanonicalFile, c.LegacySHA256, c.CanonicalSHA,
		})
	}
	return writeCSV(filepath.Join(outDir, "drift-summary.csv"), header, rows)
}

func writeGridCSV(outDir string, g []GridRow) error {
	header := []string{
		"bodies", "N", "f", "pred_dx_body0_raw", "meas_dx_body0_raw",
		"pred_total_x_raw", "meas_total_x_raw", "exact", "whole_unit_onset_f",
	}
	rows := make([][]string, 0, len(g))
	for _, r := range g {
		rows = append(rows, []string{
			strconv.Itoa(r.Bodies), strconv.Itoa(r.N), strconv.Itoa(r.F),
			strconv.FormatInt(r.PredDxRaw, 10), strconv.FormatInt(r.MeasDxBody0Raw, 10),
			strconv.FormatInt(r.PredTotalXRaw, 10), strconv.FormatInt(r.MeasTotalXRaw, 10),
			boolStr(r.Exact), strconv.FormatInt(r.WholeUnitOnsetF, 10),
		})
	}
	return writeCSV(filepath.Join(outDir, "formula-grid.csv"), header, rows)
}

func writeLatentCSV(outDir string, lat *LatentResult) error {
	header := []string{
		"f_after_checkpoint", "global_tick", "direct_hash",
		"naive_hash_before_normalize", "normalized_hash",
		"naive_hash_equal", "normalized_hash_equal",
		"dx_body0_raw", "dx_body0_units", "x_whole_unit_equal",
	}
	rows := make([][]string, 0, len(lat.Frames))
	for _, r := range lat.Frames {
		rows = append(rows, []string{
			strconv.Itoa(r.F), strconv.Itoa(r.Tick), r.DirectHash,
			r.NaiveHash, r.NormalizedHash,
			boolStr(r.HashEqualDirect), boolStr(r.NormalizedHashEqual),
			strconv.FormatInt(r.DxBody0Raw, 10), r.DxBody0UnitsExact,
			boolStr(r.XWholeUnitEqual),
		})
	}
	return writeCSV(filepath.Join(outDir, "latent-onset-frame-by-frame.csv"), header, rows)
}

func writeJSON(outDir string, rep *Report) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFile(filepath.Join(outDir, "drift-report.json"), b)
}

func mark(b bool) string {
	if b {
		return "✅一致"
	}
	return "❌不同"
}

func writeFrameComparisonMD(outDir string, cs []*CaseResult) error {
	var sb strings.Builder
	sb.WriteString("# 两版序列化结果逐帧比对\n\n")
	sb.WriteString("> “规范化前”= 新代码想当然按小端+规范序读 v1 旧快照（naive）；“规范化后”= `DecodeNormalized` 按 v1 标志用大端并反转字段。三者使用同一份指令脚本与 Options。\n\n")
	for ci, c := range cs {
		sb.WriteString(fmt.Sprintf("### %s 逐帧比对（C=%d，f=0 为载入快照当下；完整精度见 `frames/%s.frame-by-frame.csv`）\n\n",
			c.Spec.ID, c.Spec.Checkpoint, c.Spec.ID))
		sb.WriteString("| f | tick | 直跑 hash | 规范化前 hash | 规范化后 hash | 旧序=直跑 | 规范=直跑 | Δx0 raw | 位置最大偏差 raw | 位置最大偏差单位 | 公式残差 |\n")
		sb.WriteString("|---:|---:|---|---|---|:---:|:---:|---:|---:|---:|---:|\n")
		for _, r := range c.Frames {
			sb.WriteString(fmt.Sprintf(
				"| %d | %d | `%s` | `%s` | `%s` | %s | %s | %d | %d | %.6f | %d |\n",
				r.F, r.Tick, r.DirectHash, r.NaiveHash, r.NormalizedHash,
				mark(r.NaiveHashEqual), mark(r.NormalizedHashEqual),
				r.Dx0Raw, r.PosMaxAbsRaw, r.PosMaxAbsUnits, r.FormulaMaxAbsResidual,
			))
		}
		if ci < len(cs)-1 {
			sb.WriteString("\n")
		}
	}
	return writeFile(filepath.Join(outDir, "frame-comparison.md"), []byte(sb.String()))
}
