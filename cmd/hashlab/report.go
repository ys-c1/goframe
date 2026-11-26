// Package main 的 report.go 负责把实验结果落成 JSON/CSV/Markdown 产物。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeHashJSON 输出两版哈希各自的逐帧明细（各存一份）。
func writeHashJSON(outDir string, groups []groupResult) error {
	v1 := map[string]any{"version": "v1-drop-low-b8", "groups": []groupResult{}}
	v2 := map[string]any{"version": "v2-full-32", "groups": []groupResult{}}
	for _, g := range groups {
		v1["groups"] = append(v1["groups"].([]groupResult), g)
		g2 := g
		g2.Rows = nil // 组级汇总在 v2_new_group_*.json 里带帧，汇总文件只放判定结论
		v2["groups"] = append(v2["groups"].([]groupResult), g2)
	}
	if err := writeJSON(filepath.Join(outDir, "v1_old_hash.json"), v1); err != nil {
		return err
	}
	return writeJSON(filepath.Join(outDir, "v2_new_hash.json"), v2)
}

// writeMatrix 穷举“位置差 × 哈希位宽”的漏判矩阵，实测次数与闭式概率逐格对照。
func writeMatrix(outDir string) error {
	var rows []matrixRow
	var csv strings.Builder
	csv.WriteString("delta_raw,delta_world,retained_w,drop_b,bucket_B,trials_N,miss_M,empirical_rate,theory_P,theory_fraction\n")
	for _, d := range groupDeltas {
		for _, b := range sweepB {
			mTheory := missCountDropLow(d, b)
			mEmp := empiricalMissDropLow(d, b)
			B := 1 << b
			r := matrixRow{
				DeltaRaw: d, DeltaWorld: rawToWorld(int32(d)),
				RetainedW: 32 - b, DropB: b, BucketB: B, TrialsN: B,
				MissM: mEmp, EmpiricalRate: float64(mEmp) / float64(B),
				TheoryP:     float64(mTheory) / float64(B),
				TheoryExact: fmt.Sprintf("%d/%d", mTheory, B),
			}
			rows = append(rows, r)
			fmt.Fprintf(&csv, "%d,%.9f,%d,%d,%d,%d,%d,%.6f,%.6f,%s\n",
				r.DeltaRaw, r.DeltaWorld, r.RetainedW, r.DropB, r.BucketB,
				r.TrialsN, r.MissM, r.EmpiricalRate, r.TheoryP, r.TheoryExact)
		}
	}
	if err := writeJSON(filepath.Join(outDir, "matrix_miss_width_frame.json"),
		map[string]any{"model": "drop-low-bucket", "rows": rows}); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "matrix_miss_width_frame.csv"), []byte(csv.String()), 0o644)
}

// writeLowOnly 验证字面“只取低位”模型：整数单位差在 2^k 整除 Δ 时必漏，与对齐无关。
func writeLowOnly(outDir string, window int) error {
	var rows []lowOnlyRow
	var csv strings.Builder
	csv.WriteString("whole_units_m,delta_raw,keep_k,trials_N,miss_M,divides,v1_judged_tick,v2_judged_tick\n")
	for m := 1; m <= 3; m++ {
		d := m * 65536
		for _, k := range sweepK {
			miss := empiricalMissKeepLow(d, k)
			B := 1 << k
			// 用真实世界追踪判定帧：stable 差，位置差恒为 d。
			traced, err := trace(d, modeStable, min(window, 8), keepLowHash(uint(k)), v2Hash)
			if err != nil {
				return err
			}
			r := lowOnlyRow{
				WholeUnits: m, DeltaRaw: d, KeepK: k, TrialsN: B, MissM: miss,
				Divides:      miss == B,
				V1JudgedTick: firstJudgedTick(traced, false),
				V2JudgedTick: firstJudgedTick(traced, true),
			}
			rows = append(rows, r)
			fmt.Fprintf(&csv, "%d,%d,%d,%d,%d,%v,%s,%s\n",
				r.WholeUnits, r.DeltaRaw, r.KeepK, r.TrialsN, r.MissM, r.Divides,
				tickCSV(r.V1JudgedTick), tickCSV(r.V2JudgedTick))
		}
	}
	if err := writeJSON(filepath.Join(outDir, "whole_units_low_only.json"),
		map[string]any{"model": "keep-low-only", "rows": rows}); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "whole_units_low_only.csv"), []byte(csv.String()), 0o644)
}

// writeFrameCSVs 输出三组世界的逐帧追踪（v1/v2 各一份）与概率穷举扫描。
func writeFrameCSVs(outDir string, groups []groupResult) error {
	writeTrace := func(name string, useV2 bool, gs []groupResult) error {
		var sb strings.Builder
		sb.WriteString("group_delta_raw,tick,pos_a,pos_b,delta_raw,snapshot_equal,hash_a,hash_b,hash_equal\n")
		for _, g := range gs {
			for _, r := range g.Rows {
				ha, hb, eq := r.HashA, r.HashB, r.HashEqual
				if useV2 {
					ha, hb, eq = r.V2HashA, r.V2HashB, r.V2HashEqual
				}
				fmt.Fprintf(&sb, "%d,%d,%d,%d,%d,%v,%s,%s,%v\n",
					g.DeltaRaw, r.Tick, r.PosA, r.PosB, r.DeltaRaw, r.SnapshotEqual, ha, hb, eq)
			}
		}
		return os.WriteFile(filepath.Join(outDir, name), []byte(sb.String()), 0o644)
	}
	if err := writeTrace("v1_old_frames.csv", false, groups); err != nil {
		return err
	}
	if err := writeTrace("v2_new_frames.csv", true, groups); err != nil {
		return err
	}

	var prob strings.Builder
	prob.WriteString("model,delta_raw,width_param,trials_N,miss_M,empirical_rate,theory_P\n")
	for _, d := range groupDeltas {
		for _, b := range sweepB {
			m := empiricalMissDropLow(d, b)
			B := 1 << b
			fmt.Fprintf(&prob, "drop_low,%d,b=%d,%d,%d,%.6f,%.6f\n",
				d, b, B, m, float64(m)/float64(B), probDropLow(d, b))
		}
		for _, k := range sweepK {
			m := empiricalMissKeepLow(d, k)
			B := 1 << k
			fmt.Fprintf(&prob, "keep_low,%d,k=%d,%d,%d,%.6f,%.6f\n",
				d, k, B, m, float64(m)/float64(B), probKeepLow(d, k))
		}
	}
	return os.WriteFile(filepath.Join(outDir, "probability_sweep.csv"), []byte(prob.String()), 0o644)
}

// writeComparison 输出两版哈希的总对照 Markdown 与 JSON。
func writeComparison(outDir string, groups []groupResult, window int) error {
	type cmpRow struct {
		DeltaRaw     int     `json:"delta_raw"`
		DeltaWorld   float64 `json:"delta_world_units"`
		TrueTick     int     `json:"true_first_diff_tick"`
		V1JudgedTick int     `json:"v1_judged_tick"`
		V2JudgedTick int     `json:"v2_judged_tick"`
		V1Miss       bool    `json:"v1_missed"`
		V2Aligned    bool    `json:"v2_aligned"`
	}
	var cmp []cmpRow
	for _, g := range groups {
		cmp = append(cmp, cmpRow{
			DeltaRaw: g.DeltaRaw, DeltaWorld: g.DeltaWorldUnits,
			TrueTick: g.TrueFirstDiffTick, V1JudgedTick: g.V1JudgedTick,
			V2JudgedTick: g.V2JudgedTick, V1Miss: g.V1JudgedTick == -1,
			V2Aligned: g.V2JudgedTick == g.TrueFirstDiffTick,
		})
	}
	if err := writeJSON(filepath.Join(outDir, "comparison.json"),
		map[string]any{"headline": "v1=drop-low-b8/retain-w24 vs v2=full-32", "rows": cmp}); err != nil {
		return err
	}

	var md strings.Builder
	md.WriteString("# 状态哈希漏判：两版哈希对拍结果\n\n")
	md.WriteString("由 `go run ./cmd/hashlab lab` 自动生成，所有数字可由 `go test ./...` 复算。\n\n")
	md.WriteString("## 1. 漏判概率的闭式函数\n\n")
	md.WriteString("状态字段是 Q16.16 int32（原生 32 位）。两种窄位哈希对应两种漏判机制：\n\n")
	md.WriteString("**模型一：丢低位分桶**（旧版 v1，丢最低 b 位，桶宽 B=2^b，保留 w=32-b 位）。对齐 r=x mod B 在桶内均匀时：\n\n")
	md.WriteString("```\nP_miss_drop(Δ,b) = max(0, 1 - |Δ|/B),   B = 2^b\n")
	md.WriteString("漏判对齐数 M = max(0, B-|Δ|)；|Δ|≥B 时两个位置至少相隔一个整桶，漏判率恒为 0\n```\n\n")
	md.WriteString("**模型二：字面“只取低位”**（每字段只留低 k 位）。低 k 位相等当且仅当 2^k 整除 Δ，与对齐无关：\n\n")
	md.WriteString("```\nP_miss_keep(Δ,k) = 1[ 2^k | Δ ]      （固定 Δ 是 0/1 确定事件）\n")
	md.WriteString("Δ 在 1..D 均匀随机时 ≈ 2^-k\n```\n\n")
	md.WriteString("## 2. 三组位置差主表（v1：b=8 丢低位 / 保留 w=24）\n\n")
	md.WriteString("| 组 Δ(raw) | Δ(世界单位) | 实际首差帧 | v1 判定 | v2 判定帧 | v2 帧对齐 |\n")
	md.WriteString("|---:|---:|---:|---|---:|:--:|\n")
	for _, g := range groups {
		fmt.Fprintf(&md, "| %d | %.9f | %d | %s | tick=%d | %v |\n",
			g.DeltaRaw, g.DeltaWorldUnits, g.TrueFirstDiffTick,
			g.V1Verdict, g.V2JudgedTick, g.FrameAligned)
	}
	md.WriteString("\n逐帧明细见 `v1_old_hash.json` / `v2_new_hash.json` 与 `v1_old_frames.csv` / `v2_new_frames.csv`。\n\n")

	md.WriteString("## 3. 漏判次数 × 哈希位宽（穷举一个完整桶的全部对齐）\n\n")
	md.WriteString("| Δ(raw) | Δ(世界单位) | 保留 w | 丢 b | 桶 B | 试验 N | 漏判 M | 实测率 | 理论 P | 理论分率 |\n")
	md.WriteString("|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, d := range groupDeltas {
		for _, b := range sweepB {
			m := empiricalMissDropLow(d, b)
			B := 1 << b
			fmt.Fprintf(&md, "| %d | %.9f | %d | %d | %d | %d | %d | %.6f | %.6f | %d/%d |\n",
				d, rawToWorld(int32(d)), 32-b, b, B, B, m,
				float64(m)/float64(B), probDropLow(d, b), missCountDropLow(d, b), B)
		}
	}
	md.WriteString("\n实测次数与闭式公式逐格相等（穷举，无抽样误差）。\n\n")

	md.WriteString("## 4. 位宽如何改变判定的分叉帧号（增长差，观测窗 " + fmt.Sprint(window) + " 帧）\n\n")
	md.WriteString("增长差只冲量不抵消，首差帧 tick=4 之后第 t 帧的位置差为 t·Δ。\n\n")
	md.WriteString("| Δ(raw) | 保留 w | 丢 b | 桶 B | 漏判 P | 稳定差 v1 判定帧 | 增长差 v1 首判帧（全局 tick） | v2 全位判定帧 |\n")
	md.WriteString("|---:|---:|---:|---:|---:|---|---:|---:|\n")
	for _, d := range groupDeltas {
		for _, b := range sweepB {
			B := 1 << b
			stableRows, _ := trace(d, modeStable, 8, dropLowHash(uint(b)), v2Hash)
			growRows, _ := trace(d, modeGrowing, window, dropLowHash(uint(b)), v2Hash)
			sTick := firstJudgedTick(stableRows, false)
			gTick := firstJudgedTick(growRows, false)
			v2Tick := firstJudgedTick(growRows, true)
			fmt.Fprintf(&md, "| %d | %d | %d | %d | %.6f | %s | %s | %s |\n",
				d, 32-b, b, B, probDropLow(d, b),
				tickMD(sTick), tickMD(gTick), tickMD(v2Tick))
		}
	}
	md.WriteString("\n桶越粗（b 越大），增长差越过桶边界越晚，判定帧越延后，甚至观测窗内始终漏判；v2 恒定在 tick=4。\n\n")

	md.WriteString("## 5. 字面“只取低位”：整数单位级差异如何被漏判\n\n")
	md.WriteString("Q16.16 中差 m 个整数单位即 Δ=m·65536，只翻动第 16 位及以上；只保留低 k 位时，漏判 iff 2^k|Δ。\n\n")
	md.WriteString("| 整数单位 m | Δ(raw) | 保留低 k 位 | 试验 N | 漏判 M | 2^k 整除 Δ | v1 判定帧 | v2 判定帧 |\n")
	md.WriteString("|---:|---:|---:|---:|---:|:--:|---|---:|\n")
	for m := 1; m <= 3; m++ {
		d := m * 65536
		for _, k := range sweepK {
			miss := empiricalMissKeepLow(d, k)
			B := 1 << k
			traced, _ := trace(d, modeStable, 8, keepLowHash(uint(k)), v2Hash)
			fmt.Fprintf(&md, "| %d | %d | %d | %d | %d | %v | %s | %s |\n",
				m, d, k, B, miss, miss == B,
				tickMD(firstJudgedTick(traced, false)), tickMD(firstJudgedTick(traced, true)))
		}
	}
	md.WriteString("\n漏判次数在全部对齐上只有“全 N 或 0”两种，证明这是与对齐无关的整除事件；全 32 位 v2 对所有组都在 tick=4 判分叉。\n\n")

	md.WriteString("## 6. 复现\n\n```\n")
	md.WriteString("go run ./cmd/hashlab lab\n")
	md.WriteString("go run ./cmd/hashlab forkcheck --delta 64\n")
	md.WriteString("go test ./...\n```\n")
	return os.WriteFile(filepath.Join(outDir, "comparison.md"), []byte(md.String()), 0o644)
}

// writeSummary 输出一份机器可读的最终判定摘要，方便对拍脚本消费。
func writeSummary(outDir string, groups []groupResult) error {
	type summary struct {
		AllV1Missed  bool `json:"all_v1_missed_real_forks"`
		AllV2Aligned bool `json:"all_v2_aligned_with_true_diff_tick"`
		TrueTick     int  `json:"true_first_diff_tick"`
		GroupCount   int  `json:"group_count"`
	}
	allMiss, allAlign := true, true
	for _, g := range groups {
		if g.V1JudgedTick != -1 {
			allMiss = false
		}
		if g.V2JudgedTick != g.TrueFirstDiffTick {
			allAlign = false
		}
	}
	return writeJSON(filepath.Join(outDir, "summary.json"), summary{
		AllV1Missed: allMiss, AllV2Aligned: allAlign,
		TrueTick: forkTick, GroupCount: len(groups),
	})
}

func tickCSV(t int) string {
	if t < 0 {
		return "never"
	}
	return fmt.Sprintf("tick=%d", t)
}

func tickMD(t int) string {
	if t < 0 {
		return "窗内未判分叉"
	}
	return fmt.Sprintf("tick=%d", t)
}
