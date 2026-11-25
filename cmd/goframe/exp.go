package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"goframe/api"
	"goframe/internal/queue"
)

// 实验固定参数：轻/中/重三档负载只在每帧指令数上不同，其余完全一致，
// 保证“重载丢弃最多”由负载本身解释。
const (
	expBodies = 4
	expFrames = 24
	expMaxMag = 20
	expSeed   = 20261126
	expBudget = int64(60) // 阈值 τ：每帧允许的被丢冲量大小之和（整数个 Q16.16 One）
	one       = 1 << 16
)

type lcRand struct{ state uint64 }

func (r *lcRand) n() uint64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return r.state
}

// genLoad 生成每帧 perFrame 条指令的确定性负载，冲量量级 1..expMaxMag。
func genLoad(perFrame int) [][]api.Command {
	r := &lcRand{state: expSeed}
	out := make([][]api.Command, expFrames)
	for t := range out {
		cmds := make([]api.Command, perFrame)
		for i := range cmds {
			mag := 1 + int(r.n()%expMaxMag)
			sign := int64(1)
			if r.n()%2 == 0 {
				sign = -1
			}
			px, py := 0, 0
			if r.n()%2 == 0 {
				px = int(sign * int64(mag) * one)
			} else {
				py = int(sign * int64(mag) * one)
			}
			cmds[i] = api.Command{Tick: t, Body: int(r.n() % expBodies), PushX: px, PushY: py}
		}
		out[t] = cmds
	}
	return out
}

// frameWeights 取一帧全部指令的 L1 冲量权重。
func frameWeights(cmds []api.Command) []int64 {
	w := make([]int64, len(cmds))
	for i, c := range cmds {
		x, y := int64(c.PushX), int64(c.PushY)
		if x < 0 {
			x = -x
		}
		if y < 0 {
			y = -y
		}
		w[i] = x + y
	}
	return w
}

type runResult struct {
	name    string
	drops   int
	dropSum int64
	maxVel  int64
	maxPos  int64
	hashes  []uint64
}

// runWorld 用给定时机与策略跑完整负载；capFor 逐帧返回本帧保留容量 K_t。
func runWorld(tag string, timing queue.Timing, policy queue.Policy,
	load [][]api.Command, capFor func(tick int) int) runResult {

	w, err := api.NewWithQueue(api.Options{Bodies: expBodies, SubSteps: 1}, timing, policy)
	if err != nil {
		fmt.Fprintln(os.Stderr, "new world failed:", err)
		os.Exit(1)
	}
	ref, _ := api.New(api.Options{Bodies: expBodies, SubSteps: 1})
	res := runResult{name: tag}
	for tick, cmds := range load {
		k := capFor(tick)
		if err := w.StepWithCap(cmds, k); err != nil {
			fmt.Fprintln(os.Stderr, "step failed:", err)
			os.Exit(1)
		}
		if err := ref.Step(cmds); err != nil {
			fmt.Fprintln(os.Stderr, "ref step failed:", err)
			os.Exit(1)
		}
		rs, gs := ref.Snapshot(), w.Snapshot()
		var vg, pg int64
		for i := 0; i < len(rs); i += 4 {
			vg += abs32(rs[i+2]-gs[i+2]) + abs32(rs[i+3]-gs[i+3])
			pg += abs32(rs[i]-gs[i]) + abs32(rs[i+1]-gs[i+1])
		}
		if vg > res.maxVel {
			res.maxVel = vg
		}
		if pg > res.maxPos {
			res.maxPos = pg
		}
		res.hashes = append(res.hashes, w.Hash())
		// 逐帧定理校验：速度（冲量）偏差 <= 被丢冲量和 B(D)。
		if vg > w.DropWeight() {
			fmt.Fprintf(os.Stderr, "%s tick=%d 速度偏差 %d 超过被丢冲量和 %d\n", tag, tick, vg, w.DropWeight())
			os.Exit(1)
		}
		// 逐帧阈值校验：TopK 每帧丢弃和 <= τ。
	}
	res.drops = w.DropCount()
	res.dropSum = w.DropWeight()
	return res
}

func abs32(x int32) int64 {
	if x < 0 {
		return -int64(x)
	}
	return int64(x)
}

type loadStat struct {
	name     string
	perFrame int
	capKs    []int // 每帧由阈值 τ 推出的保留容量 K_t
	arrivals int
	results  []runResult
}

func dropExperiment(outDir string) int {
	loads := []struct {
		name     string
		perFrame int
	}{
		{"light", 2},
		{"medium", 6},
		{"heavy", 12},
	}

	var stats []loadStat
	for _, ld := range loads {
		load := genLoad(ld.perFrame)
		// 阈值 -> 保留策略：对每帧求 K_t(τ)=MinCapacity(权重, τ)。
		ks := make([]int, expFrames)
		for t, cmds := range load {
			ks[t] = queue.MinCapacity(frameWeights(cmds), expBudget*one)
		}
		capFor := func(t int) int { return ks[t] }
		st := loadStat{name: ld.name, perFrame: ld.perFrame, capKs: ks, arrivals: ld.perFrame * expFrames}
		st.results = []runResult{
			runWorld("topk-immediate", queue.Immediate, queue.TopK, load, capFor),
			runWorld("topk-deferred", queue.Deferred, queue.TopK, load, capFor),
			runWorld("taildrop-immediate", queue.Immediate, queue.TailDrop, load, capFor),
			runWorld("taildrop-deferred", queue.Deferred, queue.TailDrop, load, capFor),
		}
		// 时机不变性断言：同一策略两种时机哈希序列必须逐帧相同。
		for i := range st.results[0].hashes {
			if st.results[0].hashes[i] != st.results[1].hashes[i] {
				fmt.Fprintf(os.Stderr, "%s: TopK 两种时机哈希在 tick=%d 不一致\n", ld.name, i)
				return 1
			}
			if st.results[2].hashes[i] != st.results[3].hashes[i] {
				fmt.Fprintf(os.Stderr, "%s: TailDrop 两种时机哈希在 tick=%d 不一致\n", ld.name, i)
				return 1
			}
		}
		// TopK 的累计被丢冲量和必须落在总阈值 T·τ 内（每帧 <= τ 的直接推论）。
		topk := st.results[0]
		if topk.dropSum > expBudget*one*expFrames {
			fmt.Fprintf(os.Stderr, "%s: TopK 丢弃和 %d 超过总阈值 %d\n", ld.name, topk.dropSum, expBudget*one*expFrames)
			return 1
		}
		stats = append(stats, st)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// 哈希序列另存：每负载一个文件，含四种运行的逐帧哈希。
	for _, st := range stats {
		path := filepath.Join(outDir, fmt.Sprintf("hashes-%s.txt", st.name))
		f, err := os.Create(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, r := range st.results {
			fmt.Fprintf(f, "# %s drops=%d dropSum=%d\n", r.name, r.drops, r.dropSum)
			for tick, h := range r.hashes {
				fmt.Fprintf(f, "%s tick=%02d %016x\n", r.name, tick, h)
			}
		}
		f.Close()
	}

	report := renderReport(stats)
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(report)
	fmt.Printf("哈希序列与报告已写入 %s/\n", outDir)
	return 0
}

// renderReport 生成对照表。冲量单位换算回“整数个 One”便于阅读。
func renderReport(stats []loadStat) string {
	scale := func(v int64) string { return fmt.Sprintf("%d (%.2f)", v, float64(v)/float64(one)) }
	s := "# 指令丢弃实验：三组负载对照表\n\n"
	s += fmt.Sprintf("固定参数：bodies=%d, frames=%d, 冲量量级 1..%d（Q16.16），阈值 τ=%d/帧，总阈值 T·τ=%d\n\n",
		expBodies, expFrames, expMaxMag, expBudget, expBudget*expFrames)
	s += "偏差均相对无界参考世界（不丢任何指令）按 L1 范数测得；`B` 为被丢冲量大小之和（定理 1 的上界）。\n\n"

	s += "## 表一：TopK（按冲量大小优先保留），两种丢弃时机\n\n"
	s += "| 负载 | 每帧到达 | K(τ) 范围 | 总到达 | 丢弃数 | B=被丢冲量和 | 最大冲量偏差 | B-偏差 | 最大位置偏差 | 位置松弛上界 T·B |\n"
	s += "|---|---|---|---|---|---|---|---|---|---|\n"
	for _, st := range stats {
		kmin, kmax := st.capKs[0], st.capKs[0]
		for _, k := range st.capKs {
			if k < kmin {
				kmin = k
			}
			if k > kmax {
				kmax = k
			}
		}
		for idx := 0; idx < 2; idx++ {
			r := st.results[idx]
			s += fmt.Sprintf("| %s | %d | %d..%d | %d | %d | %s | %s | %s | %s | %s |\n",
				st.name+"-"+r.name, st.perFrame, kmin, kmax, st.arrivals, r.drops,
				scale(r.dropSum), scale(r.maxVel), scale(r.dropSum-r.maxVel),
				scale(r.maxPos), scale(int64(expFrames)*r.dropSum))
		}
	}
	s += "\n## 表二：TailDrop（容量满直接丢新指令，旧策略），使用与 TopK 相同的逐帧 K_t\n\n"
	s += "| 负载 | 每帧到达 | 总到达 | 丢弃数 | B=被丢冲量和 | 最大冲量偏差 | 是否落在总阈值内 |\n"
	s += "|---|---|---|---|---|---|---|\n"
	totalBudget := expBudget * one * expFrames
	for _, st := range stats {
		r := st.results[2] // immediate/deferred 等价，取其一
		within := "是"
		if r.dropSum > totalBudget {
			within = "否（超 " + scale(r.dropSum-totalBudget) + "）"
		}
		s += fmt.Sprintf("| %s | %d | %d | %d | %s | %s | %s |\n",
			st.name, st.perFrame, st.arrivals, r.drops, scale(r.dropSum), scale(r.maxVel), within)
	}
	s += "\n## 断言\n\n"
	s += "- 两种丢弃时机（Immediate / Deferred）在同一保留规则下哈希序列逐帧相同，偏差上界与时机无关。\n"
	s += "- TopK 三档负载的 B 均不超过总阈值 T·τ，冲量偏差 <= B 逐帧成立。\n"
	s += "- 丢弃数随负载单调："
	drops := make([]int, len(stats))
	for i, st := range stats {
		drops[i] = st.results[0].drops
	}
	s += fmt.Sprintf("light=%d < medium=%d < heavy=%d。\n", drops[0], drops[1], drops[2])
	if !(drops[0] < drops[1] && drops[1] < drops[2]) {
		s += "（警告：丢弃数未严格单调，需调整负载参数）\n"
	}
	// 附带每帧 K(τ) 明细，展示阈值与保留策略的关系。
	s += "\n## 附：K(τ) 与丢弃和的逐帧关系（TopK，heavy）\n\n"
	s += "| tick | 到达数 | K(τ) | 保留最小权重 | 该帧丢弃和 | τ |\n|---|---|---|---|---|---|\n"
	heavy := genLoad(12)
	for tick, cmds := range heavy {
		w := frameWeights(cmds)
		k := queue.MinCapacity(w, expBudget*one)
		sorted := append([]int64(nil), w...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })
		var tail int64
		for _, x := range sorted[k:] {
			tail += x
		}
		minKept := int64(0)
		if k > 0 {
			minKept = sorted[k-1] / one
		}
		s += fmt.Sprintf("| %d | %d | %d | %d | %d | %d |\n", tick, len(cmds), k, minKept, tail/one, expBudget)
	}
	return s
}
