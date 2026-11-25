package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goframe/api"
	"goframe/internal/measure"
	"goframe/replay"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: goframe run --frames N [--bodies N] [--substeps N] [--gravity N]")
		fmt.Fprintln(os.Stderr, "       goframe compare --out DIR [--frames N] [--substeps N] [--gravity N]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(run(os.Args[2:]))
	case "compare":
		os.Exit(compare(os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
}

func run(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	frames := fs.Int("frames", 10, "帧数")
	bodies := fs.Int("bodies", 8, "实体数")
	sub := fs.Int("substeps", 1, "子步数")
	grav := fs.Int("gravity", 0, "重力")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	opt := api.Options{Bodies: *bodies, SubSteps: *sub, Gravity: *grav}
	fs2 := make([]replay.Frame, *frames)
	for i := range fs2 {
		fs2[i] = replay.Frame{Commands: []api.Command{{Tick: i, Body: i % *bodies, PushX: 1 << 14}}}
	}
	logger, hashes, err := replay.Record(opt, fs2)
	if err != nil {
		fmt.Fprintln(os.Stderr, "record failed:", err)
		return 1
	}
	again, err := replay.Playback(logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "playback failed:", err)
		return 1
	}
	for i := range hashes {
		fmt.Printf("tick=%d hash=%016x\n", i, hashes[i])
	}
	same := len(hashes) == len(again)
	for i := 0; same && i < len(hashes); i++ {
		if hashes[i] != again[i] {
			same = false
			fmt.Printf("diverged at tick=%d\n", i)
		}
	}
	if !same {
		return 1
	}
	fmt.Println("replay identical")
	return 0
}

// compare 在三档实体数、两种负载下做全量与增量的逐位对拍、耗时测量，
// 并把两版序列分别存档。
func compare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	out := fs.String("out", "artifacts/compare", "结果目录")
	frames := fs.Int("frames", 600, "每档帧数")
	sub := fs.Int("substeps", 4, "子步数")
	rounds := fs.Int("rounds", 9, "每档计时轮数，取中位数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *frames <= 0 {
		fmt.Fprintln(os.Stderr, "frames 必须为正")
		return 2
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "创建结果目录失败:", err)
		return 1
	}

	bodiesList := []int{64, 512, 4096}
	workloads := []struct {
		label   string
		gravity int
		active  int
	}{
		{label: "sparse", gravity: 0, active: 1},
		{label: "dense", gravity: 2, active: 0},
	}
	var tiers []measure.Tier
	for _, wl := range workloads {
		for _, bodies := range bodiesList {
			cfg := measure.Config{
				Label: wl.label, Bodies: bodies, ActiveBodies: wl.active,
				Frames: *frames, SubStep: *sub, Gravity: wl.gravity, Rounds: *rounds,
			}
			t, err := measure.Run(cfg)
			if err != nil {
				fmt.Fprintln(os.Stderr, "测量失败:", err)
				return 1
			}
			tiers = append(tiers, t)
			if err := writeTierFiles(*out, t); err != nil {
				fmt.Fprintln(os.Stderr, "写结果失败:", err)
				return 1
			}
			fmt.Printf("%-6s bodies=%-5d 等价=%-5v 差异数=%d 不同位=%d 端到端差=%s 单帧指纹 %s vs %s\n",
				t.Config.Label, t.Config.Bodies, t.BitIdentical() && t.Equivalence.Equal(),
				t.TotalDiffs(), t.Compare.DiffBits,
				t.FullMinusInc(), t.FullHashPerFrame, t.IncHashPerFrame1)
		}
	}

	f, err := os.Create(filepath.Join(*out, "summary.md"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "写汇总失败:", err)
		return 1
	}
	defer f.Close()
	fmt.Fprintf(f, "# 增量哈希与全量哈希对拍\n\n")
	fmt.Fprintf(f, "## 逐位比对（差值恒为零才算等价）\n\n")
	if err := measure.WriteBitCheck(f, tiers); err != nil {
		return 1
	}
	fmt.Fprintf(f, "\n## 三档结果：耗时 / 等价判定 / 差异数\n\n")
	if err := measure.WriteSummary(f, tiers); err != nil {
		return 1
	}
	fmt.Fprintf(f, "\n## 单帧指纹耗时差随实体数\n\n")
	if err := measure.WriteHashTrend(f, tiers); err != nil {
		return 1
	}
	fmt.Fprintf(f, "\n## 增量对脏槽位数的敏感度\n\n")
	if err := measure.WriteDirtySensitivity(f, tiers); err != nil {
		return 1
	}
	fmt.Fprintf(f, "\n## 口径说明\n\n")
	fmt.Fprintf(f, "- 端到端：两种模式跑完全相同的仿真推进与指令序列，只有每帧取的指纹不同；仿真耗时同时计入两者。\n")
	fmt.Fprintf(f, "- sparse：无重力、每帧只推 1 个实体；dense：有重力，所有实体每帧都在动。\n")
	fmt.Fprintf(f, "- 两版序列分别存为 `bodies-*-full.txt` 与 `bodies-*-inc.txt`，逐帧比对见 `bodies-*-xor.txt`。\n")
	fmt.Fprintf(f, "- 实体越少，仿真与指纹的绝对耗时都越小，增量相对全量的收益越不明显。\n")

	allSame := true
	for _, t := range tiers {
		if !t.BitIdentical() || !t.Equivalence.Equal() {
			allSame = false
		}
	}
	if !allSame {
		fmt.Fprintln(os.Stderr, "存在不等价的档位")
		return 1
	}
	fmt.Printf("结果已写入 %s\n全量与增量逐位相同\n", *out)
	return 0
}

func writeTierFiles(dir string, t measure.Tier) error {
	prefix := fmt.Sprintf("bodies-%d-%s", t.Config.Bodies, t.Config.Label)
	files := []struct {
		name string
		fn   func(*os.File) error
	}{
		{prefix + "-full.txt", func(f *os.File) error { return measure.WriteSequence(f, t, "full") }},
		{prefix + "-inc.txt", func(f *os.File) error { return measure.WriteSequence(f, t, "inc") }},
		{prefix + "-xor.txt", func(f *os.File) error { return measure.WriteCompare(f, t) }},
	}
	for _, it := range files {
		f, err := os.Create(filepath.Join(dir, it.name))
		if err != nil {
			return err
		}
		if err := it.fn(f); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}
