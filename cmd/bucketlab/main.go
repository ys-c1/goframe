// Command bucketlab 复现“时间戳四舍五入分桶导致同录制末态不一致”，
// 并用向下取整分桶验证回放与直跑逐帧哈希相同。
//
// 它把结构化结果写入 -out 目录（默认 artifacts/bucketing）：
//   - results.json    全部数值结果
//   - comparison.md   三档帧长对比表
//   - hashes_F<n>.csv 每档的逐帧哈希
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"goframe/api"
	"goframe/internal/bucket"
	"goframe/replay"
)

const (
	ticks    = 16
	pushRaw  = 1 << 14 // 16384 raw Q16.16 = 0.25 定点单位
	timeUnit = "整数时间单位（可理解为 ms）"
	epsilon  = int64(1) // 两次求值/两条链路间允许的最大时间戳偏差
)

// witness 是一档帧长的全部实验结果。
type witness struct {
	FrameLength int64  `json:"frame_length"`
	BoundaryK   int64  `json:"boundary_k"`
	Timestamp   int64  `json:"timestamp"`
	BoundaryC   string `json:"boundary_c"` // c_k 的精确分数表示

	// 区间宽度结论
	HalfFrameWidth int64   `json:"half_frame_disagreement_width"` // F/2
	BandWidth      int64   `json:"ambiguity_band_width_2e"`       // 2e
	BandRatio      float64 `json:"ambiguity_band_ratio_2e_over_F"`

	// float64 重排求和见证
	FloatDeltas       []float64 `json:"float_deltas"`
	SumForward        string    `json:"sum_forward"`
	SumReversed       string    `json:"sum_reversed"`
	FloatFrameForward int64     `json:"float_frame_forward"`
	FloatFrameReverse int64     `json:"float_frame_reverse"`

	// 分叉测量
	DivergeTick      int     `json:"diverge_tick"`
	FrameOffset      int     `json:"frame_offset"`
	TimeOffsetUnits  int64   `json:"time_offset_units"` // 相邻桶锚点相差 F
	DiffAtDivergeRaw []int32 `json:"diff_at_diverge_raw_q16"`
	DiffAtEndRaw     []int32 `json:"diff_at_end_raw_q16"`
	MaxAbsAtDiverge  int32   `json:"max_abs_at_diverge_raw"`
	MaxAbsAtEnd      int32   `json:"max_abs_at_end_raw"`
	PushRaw          int     `json:"push_raw_q16"`
	PushFixedUnits   string  `json:"push_fixed_units"`

	// 各版末态哈希
	HashFloorEnd   string `json:"hash_floor_direct_end"`
	HashReplayEnd  string `json:"hash_floor_replay_end"`
	HashRoundEnd   string `json:"hash_round_half_up_end"`
	HashLoEnd      string `json:"hash_adjacent_lo_end"`
	HashHiEnd      string `json:"hash_adjacent_hi_end"`
	HashLoAtD      string `json:"hash_lo_at_diverge"`
	HashHiAtD      string `json:"hash_hi_at_diverge"`
	ReplayMatches  bool   `json:"floor_replay_matches_direct_frame_by_frame"`
	FramesCompared int    `json:"frames_compared"`
	RoundDiffers   bool   `json:"round_end_differs_from_floor"`
}

type resultsFile struct {
	TimeUnit string    `json:"time_unit"`
	Epsilon  int64     `json:"epsilon_units"`
	PushRaw  int       `json:"push_raw_q16"`
	Ticks    int       `json:"ticks"`
	Tiers    []witness `json:"tiers"`
}

func main() {
	out := flag.String("out", "artifacts/bucketing", "产物输出目录")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}

	// 同一段跨边界录制：三个时间戳分别踩在 F=8/16/32 的 c_1/c_2/c_3 上。
	tl := replay.TimedLog{
		Options:     api.Options{Bodies: 2, SubSteps: 1, Gravity: 0},
		FrameLength: 8, // 占位，每档覆盖
		Ticks:       ticks,
		Commands: []replay.TimedCommand{
			{Timestamp: 12, Body: 0, PushX: pushRaw},
			{Timestamp: 40, Body: 0, PushX: pushRaw},
			{Timestamp: 112, Body: 0, PushX: pushRaw},
		},
	}
	recordingDoc := map[string]any{
		"time_unit": timeUnit,
		"ticks":     ticks,
		"options":   tl.Options,
		"note":      "同一段录制；12/40/112 分别是 F=8/16/32 的半帧舍入边界 c1/c2/c3",
		"commands":  tl.Commands,
	}
	if err := writeJSON(filepath.Join(*out, "recording.json"), recordingDoc); err != nil {
		fatal(err)
	}

	// 每档：F、被研究的边界序号 k、对应的 float64 重排增量见证。
	tiers := []struct {
		F      int64
		k      int64
		deltas []float64
	}{
		{8, 1, []float64{0.2, 1.0, 10.0, 0.1, 0.2, 0.3, 0.2}},
		{16, 2, []float64{0.3, 2.0, 0.3, 5.0, 2.0, 0.2, 10.0, 20.0, 0.2}},
		{32, 3, []float64{100.0, 0.3, 10.0, 0.3, 1.0, 0.3, 0.1}},
	}

	rf := resultsFile{TimeUnit: timeUnit, Epsilon: epsilon, PushRaw: pushRaw, Ticks: ticks}
	var md tableBuilder

	for _, tier := range tiers {
		F, k := tier.F, tier.k
		tl.FrameLength = F
		res := analyze(*out, tl, F, k, tier.deltas)
		rf.Tiers = append(rf.Tiers, res)
		md.add(res)
		fmt.Printf("F=%2d t*=%3d k=%d 分叉 tick=%d 偏差(raw)=%v 回放一致=%v\n",
			F, res.Timestamp, k, res.DivergeTick, res.DiffAtDivergeRaw[:4], res.ReplayMatches)
	}

	if err := writeJSON(filepath.Join(*out, "results.json"), rf); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*out, "comparison.md"), []byte(md.string()), 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("产物已写入", *out)
}

func analyze(dir string, tl replay.TimedLog, F, k int64, deltas []float64) witness {
	star2 := bucket.RoundBoundary2(k, F) // 2*c_k
	if star2%2 != 0 {
		panic("选定边界必须是整数平局点")
	}
	tstar := star2 / 2

	w := witness{
		FrameLength:     F,
		BoundaryK:       k,
		Timestamp:       tstar,
		BoundaryC:       fmt.Sprintf("%d/2", star2),
		HalfFrameWidth:  F / 2,
		BandWidth:       2 * epsilon,
		BandRatio:       float64(2*epsilon) / float64(F),
		FloatDeltas:     deltas,
		TimeOffsetUnits: F,
		PushRaw:         pushRaw,
		PushFixedUnits:  fmt.Sprintf("%d/65536 = 1/4", pushRaw),
	}

	// ---- float64 重排求和见证 -------------------------------------
	sf := sum(deltas, false)
	sr := sum(deltas, true)
	w.SumForward = fmt.Sprintf("%.17g", sf)
	w.SumReversed = fmt.Sprintf("%.17g", sr)
	w.FloatFrameForward = int64(math.Floor(sf/float64(F) + 0.5))
	w.FloatFrameReverse = int64(math.Floor(sr/float64(F) + 0.5))
	if abs64(w.FloatFrameForward-w.FloatFrameReverse) != 1 {
		panic(fmt.Sprintf("F=%d float 见证未落在相邻帧: %d vs %d",
			F, w.FloatFrameForward, w.FloatFrameReverse))
	}

	// ---- 各版分桶 --------------------------------------------------
	floorLog, err := replay.BuildLog(tl, bucket.Floor)
	must(err)
	roundLog, err := replay.BuildLog(tl, bucket.RoundHalfUp)
	must(err)

	// JSON 落盘/重载后的 floor 回放。
	raw, err := json.Marshal(tl)
	must(err)
	var reloaded replay.TimedLog
	must(json.Unmarshal(raw, &reloaded))
	replayLog, err := replay.BuildLog(reloaded, bucket.Floor)
	must(err)

	hFloor, _ := runDetailed(tl.Options, floorLog.Frames)
	hReplay, _ := runDetailed(reloaded.Options, replayLog.Frames)
	hRound, _ := runDetailed(tl.Options, roundLog.Frames)

	// ---- 相邻两桶：把见证指令放在 k（lo）与 k+1（hi） --------------
	loFrames := cloneFrames(floorLog.Frames) // 这些 t* 下 floor 恰给 k
	hiFrames := cloneFrames(floorLog.Frames)
	moveWitness(loFrames, tstar, k, k, F)
	moveWitness(hiFrames, tstar, k, k+1, F)
	hLo, snapLo := runDetailed(tl.Options, loFrames)
	hHi, snapHi := runDetailed(tl.Options, hiFrames)

	d := firstDiff(hLo, hHi)
	if d != int(k) {
		panic(fmt.Sprintf("F=%d 分叉帧=%d, 期望 k=%d", F, d, k))
	}
	w.DivergeTick = d
	w.FrameOffset = 1

	w.DiffAtDivergeRaw = diffRaw(snapLo[d], snapHi[d])
	w.DiffAtEndRaw = diffRaw(snapLo[len(snapLo)-1], snapHi[len(snapHi)-1])
	w.MaxAbsAtDiverge = maxAbs(w.DiffAtDivergeRaw)
	w.MaxAbsAtEnd = maxAbs(w.DiffAtEndRaw)

	w.HashFloorEnd = fmt.Sprintf("%016x", hFloor[len(hFloor)-1])
	w.HashReplayEnd = fmt.Sprintf("%016x", hReplay[len(hReplay)-1])
	w.HashRoundEnd = fmt.Sprintf("%016x", hRound[len(hRound)-1])
	w.HashLoEnd = fmt.Sprintf("%016x", hLo[len(hLo)-1])
	w.HashHiEnd = fmt.Sprintf("%016x", hHi[len(hHi)-1])
	w.HashLoAtD = fmt.Sprintf("%016x", hLo[d])
	w.HashHiAtD = fmt.Sprintf("%016x", hHi[d])

	w.FramesCompared = len(hFloor)
	w.ReplayMatches = equalU64(hFloor, hReplay)
	w.RoundDiffers = w.HashRoundEnd != w.HashFloorEnd
	if !w.ReplayMatches {
		panic(fmt.Sprintf("F=%d floor 回放与直跑未逐帧一致", F))
	}

	// 逐帧哈希 CSV。
	writeFrameHashCSV(filepath.Join(dir, fmt.Sprintf("hashes_F%d.csv", F)),
		hFloor, hReplay, hRound, hLo, hHi)

	return w
}

// ---- 运行内核并保留每帧状态切片 -----------------------------------

func runDetailed(opt api.Options, frames []replay.Frame) ([]uint64, [][]int32) {
	w, err := api.New(opt)
	must(err)
	hs := make([]uint64, 0, len(frames))
	snaps := make([][]int32, 0, len(frames))
	for _, f := range frames {
		must(w.Step(f.Commands))
		hs = append(hs, w.Hash())
		snaps = append(snaps, append([]int32(nil), w.Snapshot()...))
	}
	return hs, snaps
}

// ---- 见证指令在相邻桶间移动 ---------------------------------------

func moveWitness(frames []replay.Frame, tstar, srcBucket, dstBucket, F int64) {
	idx := -1
	for i, c := range frames[srcBucket].Commands {
		_ = i
		if c.Body == 0 && c.PushX == pushRaw {
			idx = i
			break
		}
	}
	if idx < 0 {
		panic(fmt.Sprintf("src frame %d 找不到 t*=%d 的见证指令", srcBucket, tstar))
	}
	cmd := frames[srcBucket].Commands[idx]
	cmd.Tick = int(dstBucket)
	cs := frames[srcBucket].Commands
	frames[srcBucket].Commands = append(append([]api.Command(nil), cs[:idx]...), cs[idx+1:]...)
	frames[dstBucket].Commands = append(frames[dstBucket].Commands, cmd)
}

func cloneFrames(in []replay.Frame) []replay.Frame {
	out := make([]replay.Frame, len(in))
	for i, f := range in {
		out[i].Commands = append([]api.Command(nil), f.Commands...)
	}
	return out
}

// ---- 小工具 --------------------------------------------------------

func sum(xs []float64, reverse bool) float64 {
	s := 0.0
	if !reverse {
		for _, x := range xs {
			s += x
		}
	} else {
		for i := len(xs) - 1; i >= 0; i-- {
			s += xs[i]
		}
	}
	return s
}

func firstDiff(a, b []uint64) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return min(len(a), len(b))
	}
	return -1
}

func diffRaw(a, b []int32) []int32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]int32, n)
	for i := 0; i < n; i++ {
		out[i] = a[i] - b[i]
	}
	return out
}

func maxAbs(xs []int32) int32 {
	var m int32
	for _, x := range xs {
		if x < 0 {
			x = -x
		}
		if x > m {
			m = x
		}
	}
	return m
}

func equalU64(a, b []uint64) bool {
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

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bucketlab:", err)
	os.Exit(1)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func writeFrameHashCSV(path string, floor, replayH, round, lo, hi []uint64) {
	f, err := os.Create(path)
	must(err)
	defer f.Close()
	cw := csv.NewWriter(f)
	must(cw.Write([]string{"tick", "hash_floor_direct", "hash_floor_replay",
		"hash_round_half_up", "hash_adjacent_lo", "hash_adjacent_hi"}))
	for i := range floor {
		must(cw.Write([]string{
			fmt.Sprintf("%d", i),
			fmt.Sprintf("%016x", floor[i]),
			fmt.Sprintf("%016x", replayH[i]),
			fmt.Sprintf("%016x", round[i]),
			fmt.Sprintf("%016x", lo[i]),
			fmt.Sprintf("%016x", hi[i]),
		}))
	}
	cw.Flush()
	must(cw.Error())
}

// ---- Markdown 对比表 ----------------------------------------------

type tableBuilder struct{ rows []witness }

func (t *tableBuilder) add(w witness) { t.rows = append(t.rows, w) }

func (t *tableBuilder) string() string {
	var b []byte
	p := func(s string) { b = append(b, s...) }

	p("# 三档帧长对比（由 `go run ./cmd/bucketlab` 生成）\n\n")
	p("时间戳单位：整数时间单位；扰动半径 e=1；冲量 PushX=16384 raw Q16.16（=0.25 定点单位）。\n\n")

	p("## 1. 边界、区间宽度与 float64 重排见证\n\n")
	p("| F | 边界 k | t*=c_k | F/2 半帧分歧区间宽 | 模糊带宽 2e | 2e/F | float 顺序A→帧 | 逆序重排→帧 |\n")
	p("|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, w := range t.rows {
		p(fmt.Sprintf("| %d | %d | %d | %d | %d | %.4f | %s→%d | %s→%d |\n",
			w.FrameLength, w.BoundaryK, w.Timestamp, w.HalfFrameWidth, w.BandWidth, w.BandRatio,
			w.SumForward, w.FloatFrameForward, w.SumReversed, w.FloatFrameReverse))
	}
	p("\n")

	p("## 2. 分叉帧号与偏差量（相邻两桶 k vs k+1）\n\n")
	p("| F | 首个分叉 tick | 帧偏移 | 时间戳锚点差 | 分叉 tick 状态差 [px,py,vx,vy] raw | 最大绝对差 | 末态差 [px,py,vx,vy] raw | 末态最大绝对差 |\n")
	p("|---:|---:|---:|---:|---|---:|---|---:|\n")
	for _, w := range t.rows {
		d := w.DiffAtDivergeRaw[:4]
		e := w.DiffAtEndRaw[:4]
		p(fmt.Sprintf("| %d | %d | %d | %d | [%d,%d,%d,%d] | %d | [%d,%d,%d,%d] | %d |\n",
			w.FrameLength, w.DivergeTick, w.FrameOffset, w.TimeOffsetUnits,
			d[0], d[1], d[2], d[3], w.MaxAbsAtDiverge,
			e[0], e[1], e[2], e[3], w.MaxAbsAtEnd))
	}
	p("\nraw Q16.16 除以 65536 得定点单位：分叉 tick 的 px/vx 各差 16384/65536=0.25；" +
		"末态速度差追平为 0，位置差永久保留 0.25 单位。\n\n")

	p("## 3. 两版/各路径哈希对比（末态）\n\n")
	p("| F | floor 直跑末哈希 | floor 回放末哈希 | 逐帧一致 | 四舍五入末哈希 | 舍入≠floor | 相邻 lo 末哈希 | 相邻 hi 末哈希 |\n")
	p("|---:|---|---|:---:|---|:---:|---|---|\n")
	for _, w := range t.rows {
		match := "否"
		if w.ReplayMatches {
			match = "是"
		}
		diff := "相同"
		if w.RoundDiffers {
			diff = "不同"
		}
		p(fmt.Sprintf("| %d | `%s` | `%s` | %s | `%s` | %s | `%s` | `%s` |\n",
			w.FrameLength, w.HashFloorEnd, w.HashReplayEnd, match,
			w.HashRoundEnd, diff, w.HashLoEnd, w.HashHiEnd))
	}
	p("\n## 4. 分叉点两版哈希\n\n")
	p("| F | 分叉 tick | lo 哈希 | hi 哈希 |\n|---:|---:|---|---|\n")
	for _, w := range t.rows {
		p(fmt.Sprintf("| %d | %d | `%s` | `%s` |\n",
			w.FrameLength, w.DivergeTick, w.HashLoAtD, w.HashHiAtD))
	}
	p("\n逐帧哈希另见 `hashes_F8.csv`、`hashes_F16.csv`、`hashes_F32.csv`。\n")
	return string(b)
}
