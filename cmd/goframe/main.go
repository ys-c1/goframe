package main

import (
	"flag"
	"fmt"
	"os"

	"goframe/api"
	"goframe/replay"
	"goframe/verify"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: goframe <run|crosscheck> [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(run(os.Args[2:]))
	case "crosscheck":
		os.Exit(crosscheck(os.Args[2:]))
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

// crosscheck 在同一段指令上跑 sub=1/2/4 与边界档 8，逐帧比对哈希，
// 并把“sub=1 vs sub=4 哈希差值为零”作为独立一条逐帧打印。
func crosscheck(args []string) int {
	fs := flag.NewFlagSet("crosscheck", flag.ContinueOnError)
	frames := fs.Int("frames", 20, "帧数")
	bodies := fs.Int("bodies", 8, "实体数")
	grav := fs.Int("gravity", 2, "重力")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cmds := verify.CanonicalFrames(*frames, *bodies)
	m, runs, err := verify.BuildMatrix(*bodies, *grav, cmds)
	if err != nil {
		fmt.Fprintln(os.Stderr, "crosscheck failed:", err)
		return 1
	}

	fmt.Println("# 同一帧末态哈希：sub=1 vs sub=4 逐帧差值（独立检查项）")
	allZero := true
	for i := 0; i < *frames; i++ {
		h1, h4 := runs[1].Hashes[i], runs[4].Hashes[i]
		delta := h1 - h4
		if delta != 0 {
			allZero = false
		}
		fmt.Printf("tick=%02d sub1=%016x sub4=%016x delta=%d\n", i, h1, h4, delta)
	}

	fmt.Println()
	fmt.Printf("1/2/4 same-frame identical : %v\n", m.SameFrame124)
	fmt.Printf("1 vs 4 every delta zero    : %v\n", m.Pair1v4.AllDeltaZero && allZero)
	fmt.Printf("1 vs 4 raw state identical : %v\n", m.Pair1v4.AllRawEqual)
	fmt.Printf("boundary sub=8 identical   : %v\n", m.Boundary8Same)

	if !m.SameFrame124 || !m.Pair1v4.AllDeltaZero || !allZero ||
		!m.Pair1v4.AllRawEqual || !m.Boundary8Same {
		fmt.Println("CROSSCHECK FAIL")
		return 1
	}
	fmt.Println("CROSSCHECK PASS: substeps 1/2/4/8 produce identical per-frame hashes")
	return 0
}
