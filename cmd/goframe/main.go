package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goframe/api"
	"goframe/internal/parallel"
	"goframe/replay"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: goframe run|parallelcheck [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(run(os.Args[2:]))
	case "parallelcheck":
		os.Exit(parallelCheck(os.Args[2:]))
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

func parallelCheck(args []string) int {
	fs := flag.NewFlagSet("parallelcheck", flag.ContinueOnError)
	bodies := fs.Int("bodies", 12, "实体数 N")
	frames := fs.Int("frames", 16, "帧数 T")
	sub := fs.Int("substeps", 2, "每帧子步数")
	grav := fs.Int("gravity", 2, "重力（Q16.16 原始值）")
	shift := fs.Uint("coupling-shift", 2, "环上耦合冲量右移位数")
	logPath := fs.String("log", "artifacts/parallel_check.log", "文本日志输出路径")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := parallel.ScenarioConfig{
		Bodies:        *bodies,
		Frames:        *frames,
		SubSteps:      *sub,
		Gravity:       *grav,
		CouplingShift: *shift,
	}
	rep := parallel.RunExperiment(cfg, []int{1, 2, 4})
	log := rep.RenderLog()
	fmt.Print(log)

	if err := os.MkdirAll(filepath.Dir(*logPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir log dir failed:", err)
		return 1
	}
	if err := os.WriteFile(*logPath, []byte(log), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write log failed:", err)
		return 1
	}
	fmt.Printf("日志已保存: %s\n", *logPath)

	// 退出码：修复版三档并发度末态哈希必须全部等于串行，且上界必须全部成立。
	failed := false
	if rep.SerialNaiveHash != rep.SerialStagedHash {
		fmt.Println("FAIL: 两阶段计划串行语义与朴素计划不一致")
		failed = true
	}
	for _, lr := range rep.Levels {
		if _, ok := rep.BoundOK(&lr); !ok {
			fmt.Printf("FAIL: W=%d 实测差异 D=%d 超过上界\n", lr.Workers, lr.NaiveDistance)
			failed = true
		}
		if lr.StagedHash != rep.SerialNaiveHash || lr.StagedDistance != 0 {
			fmt.Printf("FAIL: W=%d 修复版并发末态与串行不一致\n", lr.Workers)
			failed = true
		}
		if lr.StagedParallelErr != nil {
			fmt.Printf("FAIL: W=%d 修复版被错误拒绝: %v\n", lr.Workers, lr.StagedParallelErr)
			failed = true
		}
		if lr.Workers > 1 && lr.NaiveParallelErr == nil {
			fmt.Printf("FAIL: W=%d 朴素计划存在共享写入却未被并发执行器拒绝\n", lr.Workers)
			failed = true
		}
	}
	if failed {
		return 1
	}
	fmt.Println("PASS: 三档并发度修复版末态哈希均与串行相同，差异上界全部成立")
	return 0
}
