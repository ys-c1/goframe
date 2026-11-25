// Command orderlab 穷举同一组交互的所有调用顺序，对照裸执行与按实体编号
// 规范化执行的末态指纹，测量每条交互在不同顺序下的冲量差，并把轨迹落盘。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/sim"
)

// group 是一组待穷举的交互及统一的初速度。
type group struct {
	name  string
	note  string
	n     int
	initV []int // Q16.16 原始值，X/Y 同初值
	sh    []sim.Shove
}

// outcome 记录一种调用顺序的执行结果。
type outcome struct {
	order    []int
	impulses []int32 // 每个交互实际施加的冲量（按执行序列记录）
	hash     uint64
	snap     []int32
}

func main() {
	one := int(fixed.One.Raw())
	groups := []group{
		{
			name:  "g1-disjoint",
			note:  "写集合不相交：两条交互各写各的实体",
			n:     4,
			initV: []int{one, 2 * one, 3 * one, 4 * one},
			sh: []sim.Shove{
				{Src: 0, Dst: 2, Gain: fixed.Value(0), Base: fixed.Value(7 * one)},
				{Src: 1, Dst: 3, Gain: fixed.Value(0), Base: fixed.Value(5 * one)},
			},
		},
		{
			name:  "g2-mutual",
			note:  "双向耦合：0->1 与 1->0 互相读对方速度，形成 2-环",
			n:     2,
			initV: []int{3 * one, 5 * one},
			sh: []sim.Shove{
				{Src: 0, Dst: 1, Gain: fixed.Value(one), Base: 0},
				{Src: 1, Dst: 0, Gain: fixed.Value(one), Base: 0},
			},
		},
		{
			name:  "g3-cycle",
			note:  "3-环：0->1、1->2、2->0，共 6 种调用顺序",
			n:     3,
			initV: []int{2 * one, 4 * one, 6 * one},
			sh: []sim.Shove{
				{Src: 0, Dst: 1, Gain: fixed.Value(one), Base: 0},
				{Src: 1, Dst: 2, Gain: fixed.Value(one), Base: 0},
				{Src: 2, Dst: 0, Gain: fixed.Value(one), Base: 0},
			},
		},
	}

	outDir := filepath.Join("artifacts", "orderlab")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	var sb strings.Builder
	sb.WriteString("# orderlab 实验汇总\n\n")
	sb.WriteString("冲量差单位为 Q16.16 原始值；哈希是 tick=0 状态指纹。\n\n")

	for _, g := range groups {
		perms := permutations(len(g.sh))
		bare := make([]outcome, 0, len(perms))
		canon := make([]outcome, 0, len(perms))
		for _, p := range perms {
			bare = append(bare, runOnce(g, p, false))
			canon = append(canon, runOnce(g, p, true))
		}

		// 轨迹留档：每种顺序一步一行。
		trajPath := filepath.Join(outDir, g.name+"-trajectory.csv")
		writeTrajectory(trajPath, g, perms, bare, canon)

		// 冲量差：同一交互在不同顺序下的冲量极差。
		diff := impulseSpread(g, perms, bare)
		bareUnique, bareHashes := uniqueHashes(bare)
		canonUnique, canonHashes := uniqueHashes(canon)

		fmt.Printf("== %s: %s\n", g.name, g.note)
		fmt.Printf("   裸执行唯一=%v 哈希数=%d  规范化唯一=%v 哈希数=%d  冲量差=%v\n\n",
			bareUnique, len(bareHashes), canonUnique, len(canonHashes), diff)

		sb.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", g.name, g.note))
		sb.WriteString("| 调用顺序 | 冲量差 | 唯一性判定 | 末态哈希 |\n")
		sb.WriteString("|---|---|---|---|\n")
		for i, p := range perms {
			label := orderLabel(p)
			d := perInteractionDiff(g, p, bare[i].impulses, bare[0].impulses)
			judge := fmt.Sprintf("裸:%s / 规范:%s", mark(bareUnique), mark(canonUnique))
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | 裸 `%016x`<br>规范 `%016x` |\n",
				label, d, judge, bare[i].hash, canon[i].hash))
		}
		sb.WriteString("\n")
	}

	// 反例：去掉全序键（只按无向实体对排序，不做决胜），规范化失效。
	sb.WriteString(writeTieBreakCounterexample(outDir))

	if err := os.WriteFile(filepath.Join(outDir, "summary.md"), []byte(sb.String()), 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("artifacts written to", outDir)
}

func runOnce(g group, order []int, canonical bool) outcome {
	st := sim.NewState(g.n, 0)
	for i, v := range g.initV {
		st.Push(i, fixed.FromRaw(v), fixed.FromRaw(v))
	}
	seq := make([]sim.Shove, len(order))
	for k, idx := range order {
		seq[k] = g.sh[idx]
	}
	if canonical {
		seq = sim.CanonicalShoves(seq)
	}
	imp := make([]int32, len(seq))
	for k, sh := range seq {
		imp[k] = st.Impulse(sh).Raw()
		st.ApplyShove(sh)
	}
	return outcome{
		order:    order,
		impulses: imp,
		hash:     hash.State(0, st),
		snap:     st.Raw(),
	}
}

// impulseSpread 计算每个交互在所有调用顺序下冲量的极差，按交互编号返回。
func impulseSpread(g group, perms [][]int, outs []outcome) []int32 {
	m := make(map[int][]int32)
	for pi, p := range perms {
		for k, idx := range p {
			m[idx] = append(m[idx], outs[pi].impulses[k])
		}
	}
	diff := make([]int32, len(g.sh))
	for idx := range g.sh {
		vs := m[idx]
		sort.Slice(vs, func(a, b int) bool { return vs[a] < vs[b] })
		diff[idx] = vs[len(vs)-1] - vs[0]
	}
	return diff
}

// perInteractionDiff 给出当前顺序相对基准顺序，每条交互（按执行位置）的冲量差。
func perInteractionDiff(g group, order []int, cur, base []int32) string {
	parts := make([]string, len(order))
	for k, idx := range order {
		parts[k] = fmt.Sprintf("I%d:%d", idx, cur[k]-base[k])
	}
	return strings.Join(parts, " ")
}

func uniqueHashes(outs []outcome) (bool, map[uint64]struct{}) {
	set := map[uint64]struct{}{}
	for _, o := range outs {
		set[o.hash] = struct{}{}
	}
	return len(set) == 1, set
}

func writeTrajectory(path string, g group, perms [][]int, bare, canon []outcome) {
	var sb strings.Builder
	sb.WriteString("mode,order,step,src,dst,gain,base,impulse_raw,vx_after_seen\n")
	for pi, p := range perms {
		writeMode := func(mode string, o outcome) {
			// 重放一次以拿到逐步速度轨迹。
			st := sim.NewState(g.n, 0)
			for i, v := range g.initV {
				st.Push(i, fixed.FromRaw(v), fixed.FromRaw(v))
			}
			seq := make([]sim.Shove, len(p))
			for k, idx := range p {
				seq[k] = g.sh[idx]
			}
			if mode == "canonical" {
				seq = sim.CanonicalShoves(seq)
			}
			for step, sh := range seq {
				j := st.Impulse(sh).Raw()
				st.ApplyShove(sh)
				sb.WriteString(fmt.Sprintf("%s,%s,%d,%d,%d,%d,%d,%d,%d\n",
					mode, orderLabel(p), step, sh.Src, sh.Dst,
					sh.Gain.Raw(), sh.Base.Raw(), j, st.VelX(sh.Dst).Raw()))
			}
		}
		writeMode("bare", bare[pi])
		writeMode("canonical", canon[pi])
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		fatal(err)
	}
}

// writeTieBreakCounterexample 构造同一 (Src,Dst) 上的两条不同自作用交互，
// 演示去掉全序键中的决胜字段后规范化不再唯一。
func writeTieBreakCounterexample(outDir string) string {
	one := int(fixed.One.Raw())
	// 自作用：读写同一实体，冲量随被写状态变化；Base 使两条仿射映射不可交换。
	shoves := []sim.Shove{
		{Src: 0, Dst: 0, Gain: fixed.Value(one), Base: 0},
		{Src: 0, Dst: 0, Gain: fixed.Value(2 * one), Base: fixed.Value(one)},
	}
	mkState := func() *sim.State {
		st := sim.NewState(1, 0)
		st.Push(0, fixed.FromRaw(one), fixed.FromRaw(one))
		return st
	}

	// 正确的全序键：含 Gain/Base 决胜，两种调用顺序归一到同一序列。
	a, b := mkState(), mkState()
	a.ApplyShovesCanonical([]sim.Shove{shoves[0], shoves[1]})
	b.ApplyShovesCanonical([]sim.Shove{shoves[1], shoves[0]})

	// 残缺键：只按 (Src,Dst) 排序，保持各自的相对先后（即未决胜）。
	broken := func(in []sim.Shove) []sim.Shove {
		out := append([]sim.Shove(nil), in...)
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Src != out[j].Src {
				return out[i].Src < out[j].Src
			}
			return out[i].Dst < out[j].Dst
		})
		return out
	}
	c, d := mkState(), mkState()
	c.ApplyShoves(broken([]sim.Shove{shoves[0], shoves[1]}))
	d.ApplyShoves(broken([]sim.Shove{shoves[1], shoves[0]}))

	hc, hd := hash.State(0, c), hash.State(0, d)
	var sb strings.Builder
	sb.WriteString("## 反例：去掉全序键的决胜字段\n\n")
	sb.WriteString("实体 0 上有两条同键自作用 I0={gain=1,base=0}、I1={gain=2,base=1}，初速度 v0=1。\n\n")
	sb.WriteString("| 规范化方式 | 调用顺序 | 末态 v0 | 哈希 | 唯一性 |\n")
	sb.WriteString("|---|---|---|---|---|\n")
	sb.WriteString(fmt.Sprintf("| 全序键(Src,Dst,Gain,Base) | I0,I1 | %d | `%016x` | %s |\n",
		a.VelX(0).Raw(), hash.State(0, a), mark(hash.State(0, a) == hash.State(0, b))))
	sb.WriteString(fmt.Sprintf("| 全序键(Src,Dst,Gain,Base) | I1,I0 | %d | `%016x` | %s |\n",
		b.VelX(0).Raw(), hash.State(0, b), mark(hash.State(0, a) == hash.State(0, b))))
	sb.WriteString(fmt.Sprintf("| 残缺键(Src,Dst) | I0,I1 | %d | `%016x` | %s |\n",
		c.VelX(0).Raw(), hc, mark(hc == hd)))
	sb.WriteString(fmt.Sprintf("| 残缺键(Src,Dst) | I1,I0 | %d | `%016x` | %s |\n\n",
		d.VelX(0).Raw(), hd, mark(hc == hd)))
	sb.WriteString("残缺键把两条交互判为并列且稳定排序保留各自相对顺序，等价于没有排序；")
	sb.WriteString("I0→I1 得 v0=7、I1→I0 得 v0=8，哈希不同，唯一性失效。\n")
	return sb.String()
}

func permutations(n int) [][]int {
	var res [][]int
	used := make([]bool, n)
	var cur []int
	var dfs func()
	dfs = func() {
		if len(cur) == n {
			res = append(res, append([]int(nil), cur...))
			return
		}
		for i := 0; i < n; i++ {
			if used[i] {
				continue
			}
			used[i] = true
			cur = append(cur, i)
			dfs()
			cur = cur[:len(cur)-1]
			used[i] = false
		}
	}
	dfs()
	return res
}

func orderLabel(p []int) string {
	parts := make([]string, len(p))
	for i, idx := range p {
		parts[i] = fmt.Sprintf("I%d", idx)
	}
	return strings.Join(parts, "->")
}

func mark(unique bool) string {
	if unique {
		return "唯一"
	}
	return "不唯一"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "orderlab:", err)
	os.Exit(1)
}
