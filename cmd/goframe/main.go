package main

import (
	"flag"
	"fmt"
	"os"

	"goframe/api"
	"goframe/replay"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: goframe run --frames N [--bodies N] [--substeps N] [--gravity N]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(run(os.Args[2:]))
	case "audit":
		os.Exit(audit(os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
}

// audit 跑三组固定输入的去重审计并打印三栏留档表。
func audit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	results, err := api.RunAudit(api.AuditScenarios())
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit failed:", err)
		return 1
	}
	fmt.Print(api.FormatTable(results))
	for _, r := range results {
		fmt.Printf("%s: 原序=%016x 归并=%016x 丢弃=%016x 归并保留=%v 字段冲突=%v 丢量X=%d 丢量Y=%d 判定=%s\n",
			r.Name, r.HashOrig, r.HashMerged, r.HashDropped, r.SnapEqual, r.HasConflict, r.LostX, r.LostY, r.Verdict)
		for _, l := range r.Lost {
			fmt.Printf("  组 tick=%d body=%d d=(%d,%d) count=%d 丢弃丢量=(%d,%d)\n",
				l.Tick, l.Body, l.DX, l.DY, l.Count, l.LostX, l.LostY)
		}
	}
	return 0
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
