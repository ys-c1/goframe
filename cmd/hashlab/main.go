// hashlab 是状态哈希漏判实验台：
//
//	go run ./cmd/hashlab lab [--out DIR] [--window N]
//	    构造三组真实分叉世界，穷举漏判概率 × 哈希位宽矩阵，写出全部产物；
//	go run ./cmd/hashlab forkcheck --delta RAW [--window N]
//	    最小逐帧见证：v1 窄位哈希把真实分叉判成一致，v2 全位哈希在实际首差帧判分叉。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// 三组位置差（raw），都小于主表桶宽 256，覆盖桶内小差异的三种对齐距离；
// 整数单位级（2^16 量级）的漏判由 writeLowOnly 的“只取低位”模型单独演示。
var groupDeltas = []int{1, 64, 192}

// 主实验位宽：b 为丢弃的低位位数，保留位宽 w=32-b。
var sweepB = []int{4, 8, 12, 16}

// 字面“只取低位”实验扫描的保留位宽 k。
var sweepK = []int{8, 16, 17, 24}

const headlineB = 8 // 主表固定桶宽 b=8（保留 w=24）

type groupResult struct {
	DeltaRaw          int        `json:"delta_raw"`
	DeltaWorldUnits   float64    `json:"delta_world_units"`
	Mode              string     `json:"mode"`
	TrueFirstDiffTick int        `json:"true_first_position_diff_tick"`
	V1JudgedTick      int        `json:"v1_judged_divergence_tick"` // -1 表示观测窗内始终判一致
	V2JudgedTick      int        `json:"v2_judged_divergence_tick"`
	V1Verdict         string     `json:"v1_verdict"`
	V2Verdict         string     `json:"v2_verdict"`
	FrameAligned      bool       `json:"v2_frame_aligned_with_true_diff"`
	Rows              []FrameRow `json:"frames"`
}

type matrixRow struct {
	DeltaRaw      int     `json:"delta_raw"`
	DeltaWorld    float64 `json:"delta_world_units"`
	RetainedW     int     `json:"retained_w"`
	DropB         int     `json:"drop_low_b"`
	BucketB       int     `json:"bucket_B"`
	TrialsN       int     `json:"trials_N"`
	MissM         int     `json:"miss_M"`
	EmpiricalRate float64 `json:"empirical_rate"`
	TheoryP       float64 `json:"theory_P"`
	TheoryExact   string  `json:"theory_exact_fraction"`
}

type lowOnlyRow struct {
	WholeUnits   int  `json:"whole_units_m"`
	DeltaRaw     int  `json:"delta_raw"`
	KeepK        int  `json:"keep_low_k"`
	TrialsN      int  `json:"trials_N"`
	MissM        int  `json:"miss_M"`
	Divides      bool `json:"2^k_divides_delta"`
	V1JudgedTick int  `json:"v1_judged_tick"`
	V2JudgedTick int  `json:"v2_judged_tick"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "lab":
		os.Exit(runLab(os.Args[2:]))
	case "forkcheck":
		os.Exit(runForkcheck(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hashlab lab [--out DIR] [--window N] | forkcheck --delta RAW [--window N]")
}

func runLab(args []string) int {
	fs := flag.NewFlagSet("lab", flag.ContinueOnError)
	out := fs.String("out", "artifacts/hashlab", "产物目录")
	window := fs.Int("window", 80, "观测窗帧数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		return 1
	}

	v1Old := dropLowHash(headlineB)
	var groups []groupResult
	for _, d := range groupDeltas {
		rows, err := trace(d, modeStable, 8, v1Old, v2Hash)
		if err != nil {
			fmt.Fprintln(os.Stderr, "trace:", err)
			return 1
		}
		trueTick := firstTrueDiffTick(rows)
		v1Tick := firstJudgedTick(rows, false)
		v2Tick := firstJudgedTick(rows, true)
		g := groupResult{
			DeltaRaw:          d,
			DeltaWorldUnits:   rawToWorld(int32(d)),
			Mode:              "stable",
			TrueFirstDiffTick: trueTick,
			V1JudgedTick:      v1Tick,
			V2JudgedTick:      v2Tick,
			V1Verdict:         verdict(v1Tick),
			V2Verdict:         verdict(v2Tick),
			FrameAligned:      v2Tick == trueTick,
			Rows:              rows,
		}
		groups = append(groups, g)
		if err := writeJSON(filepath.Join(*out, fmt.Sprintf("v1_old_group_%d.json", d)),
			map[string]any{"version": "v1-drop-low-b8", "group": g}); err != nil {
			return 1
		}
	}

	// v2 视角的同三组世界（哈希列换成全位版本输出，逐帧行结构一致）。
	for _, d := range groupDeltas {
		rows, err := trace(d, modeStable, 8, v2Hash, v2Hash)
		if err != nil {
			fmt.Fprintln(os.Stderr, "trace v2:", err)
			return 1
		}
		if err := writeJSON(filepath.Join(*out, fmt.Sprintf("v2_new_group_%d.json", d)),
			map[string]any{"version": "v2-full-32", "group": groupResult{
				DeltaRaw: d, DeltaWorldUnits: rawToWorld(int32(d)), Mode: "stable",
				TrueFirstDiffTick: firstTrueDiffTick(rows),
				V2JudgedTick:      firstJudgedTick(rows, true),
				V2Verdict:         verdict(firstJudgedTick(rows, true)),
				FrameAligned:      firstJudgedTick(rows, true) == firstTrueDiffTick(rows),
				Rows:              rows,
			}}); err != nil {
			return 1
		}
	}

	if err := writeHashJSON(*out, groups); err != nil {
		return 1
	}
	if err := writeMatrix(*out); err != nil {
		return 1
	}
	if err := writeLowOnly(*out, *window); err != nil {
		return 1
	}
	if err := writeFrameCSVs(*out, groups); err != nil {
		return 1
	}
	if err := writeComparison(*out, groups, *window); err != nil {
		return 1
	}
	if err := writeSummary(*out, groups); err != nil {
		return 1
	}
	fmt.Println("lab done ->", *out)
	return 0
}

func runForkcheck(args []string) int {
	fs := flag.NewFlagSet("forkcheck", flag.ContinueOnError)
	delta := fs.Int("delta", 1, "位置差（Q16.16 raw）")
	window := fs.Int("window", 8, "观测帧数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rows, err := trace(*delta, modeStable, *window, dropLowHash(headlineB), v2Hash)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trace:", err)
		return 1
	}
	fmt.Printf("forkcheck: delta=%d raw (%.6f world units), v1=drop-low-%d (retained w=%d)\n",
		*delta, rawToWorld(int32(*delta)), headlineB, 32-headlineB)
	fmt.Println("tick  posA  posB  dPos  actualDiff | v1(drop-low) | v2(full)")
	ok := true
	for _, r := range rows {
		actual, v1s, v2s := "    ", "equal", "equal"
		if !r.SnapshotEqual {
			actual = "YES "
		}
		if !r.HashEqual {
			v1s = "DIVERGE"
		}
		if !r.V2HashEqual {
			v2s = "DIVERGE"
		}
		fmt.Printf("%4d %5d %5d %5d  %-9s | %-12s | %s\n",
			r.Tick, r.PosA, r.PosB, r.DeltaRaw, actual, v1s, v2s)
		if !r.SnapshotEqual && r.HashEqual && !r.V2HashEqual {
			// 期望形态：真实不同、v1 漏判、v2 抓到
		}
	}
	trueTick := firstTrueDiffTick(rows)
	v1Tick := firstJudgedTick(rows, false)
	v2Tick := firstJudgedTick(rows, true)
	fmt.Println("---")
	fmt.Println("实际首个位置差异帧 =", trueTick)
	fmt.Println("v1 窄位哈希判定分叉帧 =", tickName(v1Tick))
	fmt.Println("v2 全位哈希判定分叉帧 =", tickName(v2Tick))
	if v1Tick == -1 && v2Tick == trueTick {
		fmt.Println("PASS: v1 把真实分叉误判为一致；纳入低位后的 v2 在实际首差帧准确判分叉")
	} else {
		fmt.Println("FAIL: 未呈现预期的漏判/帧对齐形态")
		ok = false
	}
	if !ok {
		return 1
	}
	return 0
}

func verdict(tick int) string {
	if tick < 0 {
		return "一致（漏判真实分叉）"
	}
	return fmt.Sprintf("分叉（tick=%d）", tick)
}

func tickName(tick int) string {
	if tick < 0 {
		return "never (判一致/漏判)"
	}
	return fmt.Sprintf("tick=%d", tick)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
