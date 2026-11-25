package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goframe/api"
	"goframe/internal/drift"
	"goframe/internal/fixed"
	"goframe/replay"
)

// tierRow 是并排对比表的一行（某个定点位档位）。
type tierRow struct {
	Bits            int     `json:"bits"`
	StepReal        string  `json:"stepReal"`
	StepRaw         int64   `json:"stepRaw"`
	LogFile         string  `json:"logFile"`
	Bytes           int64   `json:"bytes"`
	RatioToFull     float64 `json:"ratioToFull"`
	MeasuredPosRaw  int64   `json:"measuredPosDriftRaw"`
	MeasuredPosUnit float64 `json:"measuredPosDriftUnits"`
	FinalPosRaw     int64   `json:"finalPosDriftRaw"`
	BoundRaw        int64   `json:"boundRaw"`
	BoundUnit       float64 `json:"boundUnits"`
	CapUnit         int     `json:"capUnits"`
	UnderCap        bool    `json:"underCap"`
	WithinBound     bool    `json:"withinBound"`
	ReplayIdentical bool    `json:"replayIdentical"`
}

type resultFile struct {
	Params          map[string]any `json:"params"`
	CapRaw          int64          `json:"capRaw"`
	WeightSum       int64          `json:"weightSum"`
	QContinuousRaw  float64        `json:"qMaxContinuousRaw"`
	QMaxIntegerRaw  int64          `json:"qMaxIntegerRaw"`
	QMaxPower2Raw   int64          `json:"qMaxPowerOfTwoRaw"`
	RecommendedBits int            `json:"recommendedBits"`
	BackSub         backSub        `json:"backSubstitution"`
	FullBytes       int64          `json:"fullBytes"`
	Tiers           []tierRow      `json:"tiers"`
	Sweep           []tierRow      `json:"bitsSweep4to12"`
	Amplification   []ampRow       `json:"amplificationProof"`
	Originals       []origFile     `json:"originalCopies"`
}

// ampRow 是“误差经积分放大”的最坏情况饱和实验一行：
// 人为让每帧量化误差同向且等于 Emax，实测漂移必须精确等于理论界。
type ampRow struct {
	StepRaw       int64 `json:"stepRaw"`
	Frames        int   `json:"frames"`
	FrameLenDt    int   `json:"frameLenDt"`
	Emax          int64 `json:"emax"`
	WeightSum     int64 `json:"weightSum"`
	AmplifyFactor int64 `json:"amplifyFactorVersusOneStepError"`
	BoundRaw      int64 `json:"boundRaw"`
	MeasuredRaw   int64 `json:"measuredRaw"`
	Tight         bool  `json:"exactlySaturatesBound"`
}

type origFile struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

type backSub struct {
	FormulaHitsCap       bool  `json:"formulaHitsCap"`
	BoundAtQMaxInt       int64 `json:"boundAtQMaxIntegerRaw"`
	CapRaw               int64 `json:"capRaw"`
	QPlusOneExceeds      bool  `json:"qPlusOneExceedsCap"`
	EmpiricalDriftRaw    int64 `json:"empiricalDriftAtQMaxRaw"`
	EmpiricalUnderCap    bool  `json:"empiricalUnderCap"`
	WorstCaseDriftAtQMax int64 `json:"worstCaseDriftAtQMaxRaw"`
	WorstCaseUnderCap    bool  `json:"worstCaseUnderCap"`
}

func runQuant(args []string) int {
	fs := flag.NewFlagSet("quant", flag.ContinueOnError)
	frames := fs.Int("frames", 20, "帧数 N（累积时长）")
	bodies := fs.Int("bodies", 512, "实体数 B（每帧每实体一条确定性指令）")
	substeps := fs.Int("substeps", 4, "每帧子步数，即帧长 dt（单位积分子步）")
	gravity := fs.Int("gravity", 2, "重力整数部分")
	capUnits := fs.Int("cap-units", 8, "位置漂移上限 Δ（定点单位）")
	out := fs.String("out", "artifacts", "产物输出目录")
	tierFlag := fs.String("tiers", "6,8,10", "三档定点小数位，逗号分隔（步长为 2^-k）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var tiers []int
	for _, s := range strings.Split(*tierFlag, ",") {
		var k int
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &k); err != nil {
			fmt.Fprintln(os.Stderr, "bad --tiers:", *tierFlag)
			return 2
		}
		tiers = append(tiers, k)
	}
	if len(tiers) != 3 {
		fmt.Fprintln(os.Stderr, "--tiers 必须恰好三档")
		return 2
	}

	opt := api.Options{Bodies: *bodies, SubSteps: *substeps, Gravity: *gravity}
	log := replay.Log{Options: opt, Frames: buildDenseFrames(*frames, *bodies)}

	logsDir := filepath.Join(*out, "logs")
	origDir := filepath.Join(logsDir, "originals")
	if err := os.MkdirAll(origDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// ---- v1 全精度日志：两版日志中的“第一版”，留一份原件 ----
	var fullBuf bytes.Buffer
	fullN, err := replay.WriteJSONL(&fullBuf, opt, log.Frames)
	if err != nil {
		fmt.Fprintln(os.Stderr, "write full:", err)
		return 1
	}
	fullPath := filepath.Join(logsDir, "v1-full.jsonl")
	fullOrigPath := filepath.Join(origDir, "v1-full.original.jsonl")
	if err := os.WriteFile(fullPath, fullBuf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fullBytes := int64(fullN)
	fullHash, err := writeOriginalCopy(fullOrigPath, fullBuf.Bytes())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	originals := []origFile{{
		Version: "v1-full-precision", Path: filepath.ToSlash(fullOrigPath),
		Bytes: fullBytes, SHA256: fullHash,
	}}

	// ---- 基准（全精度）快照 ----
	refSnaps, _, err := replay.RunSnapshots(opt, log.Frames)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reference run:", err)
		return 1
	}

	capRaw := int64(*capUnits) << 16
	res := resultFile{
		Params: map[string]any{
			"frames": *frames, "bodies": *bodies, "substeps": *substeps,
			"gravity": *gravity, "frameLenDt": *substeps, "capPositionUnits": *capUnits,
			"tiers": tiers,
		},
		CapRaw:    capRaw,
		WeightSum: drift.WeightSum(*frames, *substeps),
		FullBytes: fullBytes,
		Originals: originals,
	}
	res.QContinuousRaw = drift.MaxContinuousStepRaw(float64(capRaw), *frames, *substeps)
	res.QMaxIntegerRaw = drift.MaxIntegerStepRaw(capRaw, *frames, *substeps)
	res.QMaxPower2Raw = drift.MaxPowerOfTwoStepRaw(capRaw, *frames, *substeps)
	res.RecommendedBits = drift.FracBitsForStepRaw(res.QMaxPower2Raw)

	// ---- 三档量化 + 体积/漂移并排 ----
	for _, bits := range tiers {
		step := int(fixed.GridStep(bits))
		qLog := replay.QuantizeLog(log, bits)

		var buf bytes.Buffer
		n, werr := replay.WriteQuantizedJSONL(&buf, opt, qLog.Frames, bits, step)
		if werr != nil {
			fmt.Fprintln(os.Stderr, "write quant:", werr)
			return 1
		}
		name := fmt.Sprintf("v2-quantized-q%d.jsonl", bits)
		path := filepath.Join(logsDir, name)
		if werr := os.WriteFile(path, buf.Bytes(), 0o644); werr != nil {
			fmt.Fprintln(os.Stderr, werr)
			return 1
		}
		// 8 定点位“第二版日志”的原件在循环外无条件留存。

		qSnaps, qHashes, rerr := replay.RunSnapshots(opt, qLog.Frames)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "quant run:", rerr)
			return 1
		}
		posDrift, _, finalDrift := replay.MaxComponentDrift(refSnaps, qSnaps)
		bound := drift.BoundRaw(int64(step), *frames, *substeps)

		// 回放自洽：Record 与 Playback 指纹必须一致。
		rec, recHashes, rerr := replay.Record(opt, qLog.Frames)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "record:", rerr)
			return 1
		}
		playHashes, perr := replay.Playback(rec)
		if perr != nil || len(playHashes) != len(recHashes) {
			fmt.Fprintln(os.Stderr, "playback:", perr)
			return 1
		}
		identical := len(qHashes) == len(playHashes)
		for i := range qHashes {
			if i >= len(playHashes) || qHashes[i] != playHashes[i] {
				identical = false
			}
		}

		// 逐帧检查实测漂移不超过对应 n 的理论界。
		for t := range qSnaps {
			d, _, _ := replay.MaxComponentDrift(refSnaps[:t+1], qSnaps[:t+1])
			b := drift.BoundRaw(int64(step), t+1, *substeps)
			if d > b {
				fmt.Fprintf(os.Stderr, "tick=%d 实测漂移 %d 超过理论界 %d\n", t, d, b)
				return 1
			}
		}

		res.Tiers = append(res.Tiers, tierRow{
			Bits: bits, StepReal: fmt.Sprintf("2^-%d", bits), StepRaw: int64(step),
			LogFile: filepath.ToSlash(filepath.Join("artifacts/logs", name)),
			Bytes:   int64(n), RatioToFull: float64(n) / float64(fullBytes),
			MeasuredPosRaw: posDrift, MeasuredPosUnit: float64(posDrift) / 65536.0,
			FinalPosRaw: finalDrift, BoundRaw: bound, BoundUnit: float64(bound) / 65536.0,
			CapUnit: *capUnits, UnderCap: posDrift <= capRaw, WithinBound: posDrift <= bound,
			ReplayIdentical: identical,
		})
	}

	// ---- 第二版日志（8 定点位）无条件留一份原件 ----
	q8Step := int(fixed.GridStep(8))
	q8Log := replay.QuantizeLog(log, 8)
	var q8Buf bytes.Buffer
	if _, werr := replay.WriteQuantizedJSONL(&q8Buf, opt, q8Log.Frames, 8, q8Step); werr != nil {
		fmt.Fprintln(os.Stderr, "write q8 original:", werr)
		return 1
	}
	q8OrigPath := filepath.Join(origDir, "v2-quantized-q8.original.jsonl")
	q8Hash, werr := writeOriginalCopy(q8OrigPath, q8Buf.Bytes())
	if werr != nil {
		fmt.Fprintln(os.Stderr, werr)
		return 1
	}
	res.Originals = append(res.Originals, origFile{
		Version: "v2-quantized-q8", Path: filepath.ToSlash(q8OrigPath),
		Bytes: int64(q8Buf.Len()), SHA256: q8Hash,
	})

	// ---- 反解步长回代验证（连续式 + 整数最大网格 + 实跑） ----
	w := drift.WeightSum(*frames, *substeps)
	// D(Qc)=Qc/2*W 应恰好等于 cap。
	dAtQc := res.QContinuousRaw / 2 * float64(w)
	res.BackSub.FormulaHitsCap = absF(dAtQc-float64(capRaw)) <= 1e-6*float64(capRaw)
	res.BackSub.BoundAtQMaxInt = drift.BoundRaw(res.QMaxIntegerRaw, *frames, *substeps)
	res.BackSub.CapRaw = capRaw
	res.BackSub.QPlusOneExceeds = drift.BoundRaw(res.QMaxIntegerRaw+1, *frames, *substeps) > capRaw

	qmaxLog := replay.QuantizeLogToStep(log, int(res.QMaxIntegerRaw))
	qmSnaps, _, err := replay.RunSnapshots(opt, qmaxLog.Frames)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qmax run:", err)
		return 1
	}
	qmDrift, _, _ := replay.MaxComponentDrift(refSnaps, qmSnaps)
	res.BackSub.EmpiricalDriftRaw = qmDrift
	res.BackSub.EmpiricalUnderCap = qmDrift <= capRaw

	// 用反解出的 Q_max 跑最坏情况同向误差流：这是最强的回代验证。
	wcQMax, werr := worstCaseDrift(int(res.QMaxIntegerRaw), *frames, *substeps)
	if werr != nil {
		fmt.Fprintln(os.Stderr, "qmax worst-case run:", werr)
		return 1
	}
	res.BackSub.WorstCaseDriftAtQMax = wcQMax
	res.BackSub.WorstCaseUnderCap = wcQMax <= capRaw && wcQMax == res.BackSub.BoundAtQMaxInt

	// ---- 4..12 位扩展扫描（体积按内存中 v2 JSONL 实际字节计），作为旁证 ----
	for bits := 4; bits <= 12; bits++ {
		step := int(fixed.GridStep(bits))
		qLog := replay.QuantizeLog(log, bits)
		var buf bytes.Buffer
		n, _ := replay.WriteQuantizedJSONL(&buf, opt, qLog.Frames, bits, step)
		qSnaps, _, _ := replay.RunSnapshots(opt, qLog.Frames)
		d, _, _ := replay.MaxComponentDrift(refSnaps, qSnaps)
		bound := drift.BoundRaw(int64(step), *frames, *substeps)
		res.Sweep = append(res.Sweep, tierRow{
			Bits: bits, StepReal: fmt.Sprintf("2^-%d", bits), StepRaw: int64(step),
			Bytes: int64(n), RatioToFull: float64(n) / float64(fullBytes),
			MeasuredPosRaw: d, MeasuredPosUnit: float64(d) / 65536.0,
			BoundRaw: bound, BoundUnit: float64(bound) / 65536.0,
			CapUnit: *capUnits, UnderCap: d <= capRaw, WithinBound: d <= bound,
		})
	}

	// ---- 放大证明：构造每帧同向、幅度 Emax 的最坏情况误差，实测必须精确等于公式界 ----
	ampCases := []struct{ q, n, dt int }{
		{256, 5, 4}, {256, 10, 4}, {256, 20, 4}, {256, 40, 4}, // 随 N 二次增长
		{256, 20, 1}, {256, 20, 2}, {256, 20, 4}, {256, 20, 8}, // 随 dt 线性增长
		{1024, 20, 4}, {256, 20, 4}, {64, 20, 4}, // 随 q 线性增长（三档）
	}
	for _, c := range ampCases {
		measured, merr := worstCaseDrift(c.q, c.n, c.dt)
		if merr != nil {
			fmt.Fprintln(os.Stderr, "amplification run:", merr)
			return 1
		}
		emax := drift.MaxRoundingError(int64(c.q))
		wsum := drift.WeightSum(c.n, c.dt)
		bound := emax * wsum
		res.Amplification = append(res.Amplification, ampRow{
			StepRaw: int64(c.q), Frames: c.n, FrameLenDt: c.dt, Emax: emax,
			WeightSum: wsum, AmplifyFactor: wsum, BoundRaw: bound,
			MeasuredRaw: measured, Tight: measured == bound,
		})
	}
	ampPath := filepath.Join(*out, "amplification.csv")
	if err := writeAmpCSV(ampPath, res.Amplification); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// ---- 落盘 JSON / CSV ----
	jsonPath := filepath.Join(*out, "quant_results.json")
	jb, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(jsonPath, append(jb, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	csvPath := filepath.Join(*out, "drift_sizes.csv")
	if err := writeTierCSV(csvPath, res.Tiers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	printReport(res, fullPath, jsonPath, csvPath, ampPath)

	if !res.BackSub.FormulaHitsCap || !res.BackSub.EmpiricalUnderCap || !res.BackSub.WorstCaseUnderCap {
		fmt.Fprintln(os.Stderr, "回代验证失败")
		return 1
	}
	for _, t := range res.Tiers {
		if !t.UnderCap || !t.WithinBound || !t.ReplayIdentical {
			fmt.Fprintf(os.Stderr, "q%d 档校验未全部通过\n", t.Bits)
			return 1
		}
	}
	for _, a := range res.Amplification {
		if !a.Tight {
			fmt.Fprintf(os.Stderr, "放大公式未被精确饱和: %+v\n", a)
			return 1
		}
	}
	return 0
}

// worstCaseDrift 构造单实体、每帧一条“半格”冲量的最坏情况序列，
// 使每帧量化误差同向且绝对值恰为 Emax，返回末帧 posX 的实测漂移 raw。
func worstCaseDrift(q, n, dt int) (int64, error) {
	opt := api.Options{Bodies: 1, SubSteps: dt, Gravity: 0}
	emax := drift.MaxRoundingError(int64(q))
	frames := make([]replay.Frame, n)
	for f := 0; f < n; f++ {
		// push=Emax：偶数 Q 时是半格（tie 向上到 Q），奇数 Q 时距 0 更近，
		// 两种情况下 |Quantize(push)-push| 都恰为 Emax，且符号每帧相同。
		frames[f] = replay.Frame{Commands: []api.Command{{
			Tick: f, Body: 0, PushX: int(emax), PushY: 0,
		}}}
	}
	qLog := replay.QuantizeLogToStep(replay.Log{Options: opt, Frames: frames}, q)
	ref, _, err := replay.RunSnapshots(opt, frames)
	if err != nil {
		return 0, err
	}
	quant, _, err := replay.RunSnapshots(opt, qLog.Frames)
	if err != nil {
		return 0, err
	}
	_, _, finalDrift := replay.MaxComponentDrift(ref, quant)
	return finalDrift, nil
}

// writeOriginalCopy 先删掉可能只读的旧原件，再写入 0444 只读原件，返回 SHA256。
func writeOriginalCopy(path string, data []byte) (string, error) {
	_ = os.Chmod(path, 0o644)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o444); err != nil {
		return "", err
	}
	sum := sha256Sum(data)
	return sum, nil
}

func sha256Sum(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func writeAmpCSV(path string, rows []ampRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{
		"stepRaw", "frames", "frameLenDt", "emax", "weightSum",
		"amplifyFactor", "boundRaw", "measuredRaw", "exactlySaturatesBound",
	})
	for _, r := range rows {
		_ = w.Write([]string{
			fmt.Sprint(r.StepRaw), fmt.Sprint(r.Frames), fmt.Sprint(r.FrameLenDt),
			fmt.Sprint(r.Emax), fmt.Sprint(r.WeightSum), fmt.Sprint(r.AmplifyFactor),
			fmt.Sprint(r.BoundRaw), fmt.Sprint(r.MeasuredRaw), fmt.Sprint(r.Tight),
		})
	}
	return w.Error()
}

// buildDenseFrames 构造确定性指令流：每帧每个实体恰好一条指令，
// 保证“每实体每帧至多一条”，与漂移上界的系数一致；不使用随机数。
func buildDenseFrames(n, b int) []replay.Frame {
	frames := make([]replay.Frame, n)
	for f := 0; f < n; f++ {
		cmds := make([]api.Command, b)
		for body := 0; body < b; body++ {
			h := mix64(uint64(f)*1000003 + uint64(body)*9176 + 0x9e3779b97f4a7c15)
			px := 12288 + int(h%8192) + (f-n/2)*128 // ~0.19..0.31 单位 + 缓慢漂移
			py := int((h>>17)%16384) - 8192         // ±0.125 单位
			cmds[body] = api.Command{Tick: f, Body: body, PushX: px, PushY: py}
		}
		frames[f] = replay.Frame{Commands: cmds}
	}
	return frames
}

func mix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func writeTierCSV(path string, rows []tierRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{
		"fracBits", "stepReal", "stepRaw", "bytes", "ratioToFull",
		"measuredPosDriftRaw", "measuredPosDriftUnits", "boundRaw", "boundUnits",
		"capUnits", "underCap", "replayIdentical", "logFile",
	})
	for _, r := range rows {
		_ = w.Write([]string{
			fmt.Sprint(r.Bits), r.StepReal, fmt.Sprint(r.StepRaw), fmt.Sprint(r.Bytes),
			fmt.Sprintf("%.4f", r.RatioToFull), fmt.Sprint(r.MeasuredPosRaw),
			fmt.Sprintf("%.6f", r.MeasuredPosUnit), fmt.Sprint(r.BoundRaw),
			fmt.Sprintf("%.6f", r.BoundUnit), fmt.Sprint(r.CapUnit),
			fmt.Sprint(r.UnderCap), fmt.Sprint(r.ReplayIdentical), r.LogFile,
		})
	}
	return w.Error()
}

func printReport(r resultFile, fullPath, jsonPath, csvPath, ampPath string) {
	p := r.Params
	fmt.Println("# 指令量化漂移实验（确定性，可复现）")
	fmt.Printf("参数: N=%d 帧, dt(帧长)=%d 积分子步, B=%d 实体, gravity=%d, 漂移上限 Δ=%d 单位 (%d raw)\n",
		p["frames"], p["frameLenDt"], p["bodies"], p["gravity"], p["capPositionUnits"], r.CapRaw)
	fmt.Printf("累积权重 W=dt*N(N+1)/2 = %d\n", r.WeightSum)
	fmt.Printf("反解 q_max(连续 raw) = %.4f  ≈ %.8f 单位\n", r.QContinuousRaw, r.QContinuousRaw/65536.0)
	fmt.Printf("反解 Q_max(最大整数网格) = %d raw; 最大 2 的幂 = %d raw -> 推荐 %d 定点位\n",
		r.QMaxIntegerRaw, r.QMaxPower2Raw, r.RecommendedBits)
	fmt.Println()
	fmt.Println("| 定点位 k | 步长 q | Q(raw) | 日志字节 | 体积/全精度 | 实测位置漂移(raw) | 实测(单位) | 理论上界(单位) | 上限 | 结论 |")
	fmt.Println("|---|---|---|---:|---:|---:|---:|---:|---:|---|")
	fmt.Printf("| 全精度 v1 | - | - | %d | 100%% | 0 | 0 | - | %d | 基准 |\n", r.FullBytes, p["capPositionUnits"])
	for _, t := range r.Tiers {
		verdict := "PASS"
		if !t.UnderCap || !t.WithinBound {
			verdict = "FAIL"
		}
		fmt.Printf("| %d | %s | %d | %d | %.1f%% | %d | %.6f | %.4f | %d | %s |\n",
			t.Bits, t.StepReal, t.StepRaw, t.Bytes, 100*t.RatioToFull,
			t.MeasuredPosRaw, t.MeasuredPosUnit, t.BoundUnit, t.CapUnit, verdict)
	}
	fmt.Println()
	b := r.BackSub
	fmt.Println("## q_max 回代验证")
	fmt.Printf("- 公式回代 D(q_max)=Δ: %v (D=%.1f raw vs cap=%d)\n",
		b.FormulaHitsCap, r.QContinuousRaw/2*float64(r.WeightSum), b.CapRaw)
	fmt.Printf("- 整数最大步长 Q=%d: 理论界 %d ≤ cap %d -> %v；Q+1 越界 -> %v\n",
		r.QMaxIntegerRaw, b.BoundAtQMaxInt, b.CapRaw, b.BoundAtQMaxInt <= b.CapRaw, b.QPlusOneExceeds)
	fmt.Printf("- 用 Q=%d 实跑仿真: 实测最大位置漂移 %d raw ≤ %d -> %v\n",
		r.QMaxIntegerRaw, b.EmpiricalDriftRaw, b.CapRaw, b.EmpiricalUnderCap)
	fmt.Printf("- Q_max 最坏情况同向误差流: 漂移 %d raw == 理论界 %d 且 ≤ cap %d -> %v\n",
		b.WorstCaseDriftAtQMax, b.BoundAtQMaxInt, b.CapRaw, b.WorstCaseUnderCap)
	fmt.Println()
	fmt.Println("## 放大证明（最坏情况同向误差，实测必须精确等于界）")
	fmt.Println("| Q(raw) | N | dt | Emax | W=放大倍数 | 公式界(raw) | 实测漂移(raw) | 精确饱和 |")
	fmt.Println("|---:|---:|---:|---:|---:|---:|---:|:--:|")
	for _, a := range r.Amplification {
		fmt.Printf("| %d | %d | %d | %d | %d | %d | %d | %v |\n",
			a.StepRaw, a.Frames, a.FrameLenDt, a.Emax, a.AmplifyFactor, a.BoundRaw, a.MeasuredRaw, a.Tight)
	}
	fmt.Println()
	fmt.Println("两版日志原件（只读留存，附 SHA256）:")
	for _, o := range r.Originals {
		fmt.Printf(" - %s  %d bytes  sha256:%s\n", o.Path, o.Bytes, o.SHA256)
	}
	fmt.Println("其他产物:")
	fmt.Println(" -", fullPath)
	for _, t := range r.Tiers {
		fmt.Println(" -", t.LogFile, fmt.Sprintf("(%d bytes)", t.Bytes))
	}
	fmt.Println(" -", csvPath)
	fmt.Println(" -", ampPath)
	fmt.Println(" -", jsonPath)
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
