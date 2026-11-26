package main

import (
	"flag"
	"fmt"
	"os"

	"goframe/api"
	"goframe/internal/alloc"
	"goframe/replay"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: goframe run --frames N [--bodies N] [--substeps N] [--gravity N]\n       goframe reuse [--out PATH]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(run(os.Args[2:]))
	case "reuse":
		os.Exit(reuse(os.Args[2:]))
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
	if *bodies <= 0 {
		fmt.Fprintln(os.Stderr, "bodies must be positive")
		return 2
	}
	opt := api.Options{Bodies: *bodies, SubSteps: *sub, Gravity: *grav, SpawnsPerTick: 64}
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

func reuse(args []string) int {
	fs := flag.NewFlagSet("reuse", flag.ContinueOnError)
	out := fs.String("out", "artifacts/id-reuse.log", "留档日志路径")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	text := alloc.FormatReport(alloc.RunAll())
	fmt.Print(text)
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write log failed:", err)
		return 1
	}
	fmt.Printf("日志已留档: %s\n", *out)
	return 0
}
