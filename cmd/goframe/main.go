package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goframe/api"
	"goframe/locate"
	"goframe/replay"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: goframe <run|locate> [flags]")
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "run":
		code = run(os.Args[2:])
	case "locate":
		code = locateCmd(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		code = 2
	}
	os.Exit(code)
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

// simRun 跑完整条日志，逐帧收集指纹与状态快照。
func simRun(opt api.Options, frames []replay.Frame) ([]uint64, [][]int32, error) {
	w, err := api.New(opt)
	if err != nil {
		return nil, nil, err
	}
	hashes := make([]uint64, 0, len(frames))
	snaps := make([][]int32, 0, len(frames))
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, w.Hash())
		snaps = append(snaps, append([]int32(nil), w.Snapshot()...))
	}
	return hashes, snaps, nil
}

func buildFrames(frames, bodies int) []replay.Frame {
	out := make([]replay.Frame, frames)
	for i := range out {
		out[i] = replay.Frame{Commands: []api.Command{
			{Tick: i, Body: i % bodies, PushX: 1 << 14},
		}}
	}
	return out
}

// mutateFrames 深拷贝日志并把第 at 帧的冲量改成另一个值，模拟回放分歧。
func mutateFrames(frames []replay.Frame, at int) []replay.Frame {
	out := make([]replay.Frame, len(frames))
	for i := range frames {
		cmds := make([]api.Command, len(frames[i].Commands))
		copy(cmds, frames[i].Commands)
		out[i] = replay.Frame{Commands: cmds}
	}
	out[at].Commands[0].PushX = 3 << 14
	return out
}

func equalSnap(a, b []int32) bool {
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

// worstCase 枚举所有分歧位置，返回两种定位的最坏比较次数与四舍五入均值。
func worstCase(N int) (linWorst, binWorst, linAvg, binAvg int) {
	base := make([]uint64, N)
	for i := range base {
		base[i] = uint64(i)*1000003 + 7
	}
	linSum, binSum, cases := 0, 0, N+1
	for d := -1; d < N; d++ {
		b := append([]uint64(nil), base...)
		if d >= 0 {
			for i := d; i < N; i++ {
				b[i] ^= 0xdeadbeef
			}
		}
		lin := locate.Linear(base, b)
		bin := locate.Binary(base, b)
		if lin.Comparisons > linWorst {
			linWorst = lin.Comparisons
		}
		if bin.Comparisons > binWorst {
			binWorst = bin.Comparisons
		}
		linSum += lin.Comparisons
		binSum += bin.Comparisons
	}
	linAvg = (linSum + cases/2) / cases
	binAvg = (binSum + cases/2) / cases
	return
}

func locateCmd(args []string) int {
	fs := flag.NewFlagSet("locate", flag.ContinueOnError)
	frames := fs.Int("frames", 1000, "仿真帧数")
	bodies := fs.Int("bodies", 8, "实体数")
	sub := fs.Int("substeps", 4, "子步数")
	grav := fs.Int("gravity", 2, "重力")
	diverge := fs.Int("diverge", -1, "注入分歧的帧号，默认取帧数一半")
	out := fs.String("out", filepath.Join("artifacts", "locate-log.md"), "定位日志输出路径")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *diverge < 0 {
		*diverge = *frames / 2
	}
	if *diverge >= *frames {
		fmt.Fprintln(os.Stderr, "diverge 帧号越界")
		return 2
	}

	opt := api.Options{Bodies: *bodies, SubSteps: *sub, Gravity: *grav}
	base := buildFrames(*frames, *bodies)
	ha, sa, err := simRun(opt, base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "baseline failed:", err)
		return 1
	}
	hb, sb, err := simRun(opt, mutateFrames(base, *diverge))
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutated run failed:", err)
		return 1
	}

	lin := locate.Linear(ha, hb)
	bin := locate.Binary(ha, hb)
	if lin.Frame != *diverge || bin.Frame != *diverge {
		fmt.Fprintf(os.Stderr, "定位失败: 注入帧=%d 线性=%d 二分=%d\n",
			*diverge, lin.Frame, bin.Frame)
		return 1
	}

	// 定理一的实证：首个状态不同帧必须与首个哈希不同帧重合，前缀全部一致。
	stateFrame := -1
	for i := range ha {
		if !equalSnap(sa[i], sb[i]) {
			stateFrame = i
			break
		}
	}
	if stateFrame != lin.Frame {
		fmt.Fprintf(os.Stderr, "定理一反例: 首个哈希不同帧=%d 首个状态不同帧=%d\n",
			lin.Frame, stateFrame)
		return 1
	}

	// 两种定位逐帧枚举一致性（覆盖全部可能的分歧位置，含无分歧）。
	mismatches := 0
	for d := -1; d < *frames; d++ {
		b := append([]uint64(nil), ha...)
		if d >= 0 {
			for i := d; i < *frames; i++ {
				b[i] ^= 0xdeadbeef
			}
		}
		if locate.Linear(ha, b).Frame != locate.Binary(ha, b).Frame {
			mismatches++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# 回放分歧定位日志\n\n")
	fmt.Fprintf(&b, "- 生成时间: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- 仿真参数: frames=%d bodies=%d substeps=%d gravity=%d\n",
		*frames, *bodies, *sub, *grav)
	fmt.Fprintf(&b, "- 注入分歧: 第 %d 帧冲量 %#x -> %#x\n\n",
		*diverge, 1<<14, 3<<14)

	b.WriteString("## 结论\n\n")
	fmt.Fprintf(&b, "- 首个哈希不同帧: %d（线性扫描与二分定位一致）\n", lin.Frame)
	fmt.Fprintf(&b, "- 首个状态不同帧: %d（Snapshot 逐字节比对，与哈希帧重合，实证定理一）\n", stateFrame)
	fmt.Fprintf(&b, "- 线性扫描比较次数: %d\n", lin.Comparisons)
	fmt.Fprintf(&b, "- 二分定位比较次数: %d，对数上界 ceil(log2(N+1))=%d\n",
		bin.Comparisons, locate.Bound(*frames))
	fmt.Fprintf(&b, "- 全部分歧位置枚举（含无分歧）中两种做法帧号不一致数: %d\n\n", mismatches)

	b.WriteString("## 证明要点\n\n")
	b.WriteString("1. 指纹是 (tick, 状态字节) 的纯函数：状态相同则哈希相同，逆否为哈希不同则状态不同。\n")
	b.WriteString("2. Step 是 (当前状态, 本帧指令) 的纯函数：指令日志相同且第 i 帧状态相同，则第 i+1 帧状态相同；归纳得相等前缀，谓词 P(i)=(A[i]!=B[i]) 单调，支持二分。\n")
	b.WriteString("3. 引理合取得首个哈希不同帧 h 与首个状态不同帧 s 满足 h=s（无碰撞假设；FNV-1a 64 位碰撞可忽略，Snapshot 可兜底）。\n")
	b.WriteString("4. 二分每比较一次区间长度减半，比较次数 <= ceil(log2(N+1)) = bits.Len(N)。\n\n")

	b.WriteString("## 帧数 100 / 1000 两档对照（枚举所有分歧位置）\n\n")
	b.WriteString("| 帧数 N | 线性最坏 | 二分最坏 | 实测比值 | 理论 N/Bound(N) | 线性均值 | 二分均值 |\n")
	b.WriteString("|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, N := range []int{100, 1000} {
		lw, bw, la, ba := worstCase(N)
		fmt.Fprintf(&b, "| %d | %d | %d | %.2f | %.2f | %d | %d |\n",
			N, lw, bw, float64(lw)/float64(bw),
			float64(N)/float64(locate.Bound(N)), la, ba)
	}
	b.WriteString("\n比值接近对数与线性之比；二分最坏次数恰好达到对数上界。\n\n")

	b.WriteString("## 定位帧附近的指纹\n\n")
	b.WriteString("| 帧号 | 基线哈希 | 回放哈希 | 状态快照 |\n")
	b.WriteString("|---:|---|---|---|\n")
	for i := lin.Frame - 2; i <= lin.Frame+2 && i < *frames; i++ {
		if i < 0 {
			continue
		}
		status := "相同"
		if !equalSnap(sa[i], sb[i]) {
			status = "不同"
		}
		fmt.Fprintf(&b, "| %d | %016x | %016x | %s |\n", i, ha[i], hb[i], status)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir failed:", err)
		return 1
	}
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write log failed:", err)
		return 1
	}
	fmt.Printf("divergence frame: linear=%d binary=%d (linear cmp=%d, binary cmp=%d, bound=%d)\n",
		lin.Frame, bin.Frame, lin.Comparisons, bin.Comparisons, locate.Bound(*frames))
	fmt.Println("log written:", *out)
	return 0
}
