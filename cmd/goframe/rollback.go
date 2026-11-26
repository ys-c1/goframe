package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goframe/api"
	"goframe/internal/netcode"
	"goframe/internal/rollback"
)

func rollbackMain(args []string) int {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	bodies := fs.Int("bodies", 8, "实体数")
	outDir := fs.String("out", "artifacts/rollback", "日志输出目录")
	depth := fs.Int("depth", 5, "经验回滚深度（对照基准）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	opt := api.Options{Bodies: *bodies, SubSteps: 2, Gravity: 1}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		return 1
	}

	tiers := netcode.DefaultTiers()
	var summary []string
	summary = append(summary,
		"# 回滚深度实验汇总（帧长 50ms，固定种子 LCG，600 帧/档）",
		fmt.Sprintf("# 实体数=%d，单帧快照=%d 字节（4 个 Q16.16/实体 × %d）",
			*bodies, netcode.SnapshotBytes(*bodies), *bodies),
		"",
	)

	for _, tier := range tiers {
		res, err := netcode.RunTier(tier, opt)
		if err != nil {
			fmt.Fprintln(os.Stderr, "run tier:", err)
			return 1
		}
		// 重跑一遍 D=5 拿完整事件日志（RunTier 内部不保留引擎）。
		events, err := runWithEvents(tier, opt, *depth)
		if err != nil {
			fmt.Fprintln(os.Stderr, "events:", err)
			return 1
		}
		base := filepath.Join(*outDir, tier.Name)
		writeTrace(base+".trace.csv", res.Trace)
		writeEventLog(base+".rollback.log", tier, *depth, events)
		writeFailures(base+".failures.csv", events)

		summary = append(summary, formatTier(res, *bodies, *depth)...)
		fmt.Printf("tier=%s p99=%d帧(%dms) D*=%d  D=5: 回滚=%d 越界=%d 首次失败帧=%d\n",
			tier.Name, res.P99, res.P99*netcode.FrameMS, res.TheoreticD,
			res.Depth5.Rollbacks, res.Depth5.OOB, res.Depth5.FirstOOBTarget)
	}

	if err := os.WriteFile(filepath.Join(*outDir, "summary.md"),
		[]byte(strings.Join(summary, "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write summary:", err)
		return 1
	}
	fmt.Println("日志与汇总已写入", *outDir)
	return 0
}

// runWithEvents 重放一档轨迹并返回 D=depth 下的全部引擎事件。
func runWithEvents(tier netcode.Tier, opt api.Options, depth int) ([]rollback.Event, error) {
	trace, err := tier.SampleTrace()
	if err != nil {
		return nil, err
	}
	eng, err := rollback.New(opt, depth)
	if err != nil {
		return nil, err
	}
	type arrival struct{ target, delay int }
	buckets := make([][]arrival, len(trace)+tier.MaxDelay()+1)
	for target, d := range trace {
		buckets[target+d] = append(buckets[target+d], arrival{target, d})
	}
	for tick := 0; tick < len(trace); tick++ {
		for _, a := range buckets[tick] {
			remote := []api.Command{{
				Tick: a.target, Body: (a.target + 1) % opt.Bodies, PushY: 1 << 13,
			}}
			_ = eng.Inject(tick, a.target, remote, a.delay)
		}
		local := api.Command{Tick: tick, Body: tick % opt.Bodies, PushX: 1 << 14}
		if err := eng.Advance([]api.Command{local}); err != nil {
			return nil, err
		}
	}
	return eng.Events(), nil
}

func writeTrace(path string, trace []int) {
	var b strings.Builder
	b.WriteString("target_tick,delay_frames,arrival_tick,delay_ms\n")
	for target, d := range trace {
		fmt.Fprintf(&b, "%d,%d,%d,%d\n", target, d, target+d, d*netcode.FrameMS)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write trace:", err)
	}
}

func writeEventLog(path string, tier netcode.Tier, depth int, events []rollback.Event) {
	var b strings.Builder
	fmt.Fprintf(&b, "# tier=%s seed=%d depth=%d frames=%d 帧长=%dms\n",
		tier.Name, tier.Seed, depth, tier.Frames, netcode.FrameMS)
	b.WriteString("# kind,at_tick,target_tick,delay_frames,window_lo,window_hi,hash,note\n")
	for _, ev := range events {
		fmt.Fprintf(&b, "%s,%d,%d,%d,%d,%d,%016x,%s\n",
			ev.Kind, ev.AtTick, ev.Target, ev.Delay, ev.WindowLo, ev.WindowHi,
			ev.Hash, ev.Note)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write log:", err)
	}
}

func writeFailures(path string, events []rollback.Event) {
	var b strings.Builder
	b.WriteString("failed_target_tick,arrival_tick,delay_frames,window_lo,window_hi,note\n")
	for _, ev := range events {
		if ev.Kind != rollback.EventOutOfWind {
			continue
		}
		fmt.Fprintf(&b, "%d,%d,%d,%d,%d,%s\n",
			ev.Target, ev.AtTick, ev.Delay, ev.WindowLo, ev.WindowHi, ev.Note)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write failures:", err)
	}
}

func formatTier(r *netcode.TierResult, bodies, empDepth int) []string {
	perFrame := netcode.SnapshotBytes(bodies)
	mem := func(d int) int { return (d + 1) * perFrame }
	first := "无"
	if r.Depth5.FirstOOBTick >= 0 {
		first = fmt.Sprintf("target=tick %d（at=tick %d，延迟 %d 帧）",
			r.Depth5.FirstOOBTarget, r.Depth5.FirstOOBTick,
			r.Depth5.FirstOOBTick-r.Depth5.FirstOOBTarget)
	}
	return []string{
		fmt.Sprintf("## %s（seed=%d）", r.Tier.Name, r.Tier.Seed),
		fmt.Sprintf("- 经验分位：p50=%d帧(%dms)  p95=%d帧(%dms)  p99=%d帧(%dms)  max=%d帧(%dms)",
			r.P50, r.P50*netcode.FrameMS, r.P95, r.P95*netcode.FrameMS,
			r.P99, r.P99*netcode.FrameMS, r.MaxDelay, r.MaxDelay*netcode.FrameMS),
		fmt.Sprintf("- 理论所需深度 D*=ceil(p99/帧长)=%d 帧；D=5 的环占用 %d 字节，D* 的环占用 %d 字节",
			r.TheoreticD, mem(empDepth), mem(r.TheoreticD)),
		fmt.Sprintf("- D=%d：成功回滚 %d 次，越界失败 %d 次（%.1f%%），首个越界 %s",
			r.Depth5.Depth, r.Depth5.Rollbacks, r.Depth5.OOB,
			100*float64(r.Depth5.OOB)/float64(r.Tier.Frames), first),
		fmt.Sprintf("- D=%d：成功回滚 %d 次，越界失败 %d 次（残余尾概率 %.2f%%），失败帧=%v",
			r.DepthStar.Depth, r.DepthStar.Rollbacks, r.DepthStar.OOB,
			100*float64(r.DepthStar.OOB)/float64(r.Tier.Frames),
			r.DepthStar.FailedTicks),
		"",
	}
}
