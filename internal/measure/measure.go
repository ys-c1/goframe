// Package measure 对全量与增量哈希做计时与逐位对拍，并输出可存档的结果。
//
// 计时分两层：
//
//   - 单帧指纹：抛开仿真，只测一次指纹计算本身。全量是 O(实体数)，
//     增量是 O(变化槽位数)。这一层回答“增量到底省了多少”。
//   - 端到端：两种模式跑完全相同的仿真推进与指令序列，只有每帧取的指纹
//     不同。仿真耗时同时计入两者，所以差距会被仿真稀释。
//
// 负载分两种，因为增量是否划算取决于每帧有多少实体真的变化：
//
//   - sparse：只有少量实体被推动、无重力，脏槽位数很少；
//   - dense：有重力，所有实体每帧都在动，脏槽位数等于实体数。
//
// 两种负载都报，避免只挑好看的一种。
package measure

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"goframe/api"
	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/sim"
	"goframe/replay"
)

// Config 是一档测量配置。
type Config struct {
	Label        string // 负载标签，写进报告
	Bodies       int
	ActiveBodies int // 每帧会收到指令的实体数；0 表示全部
	Frames       int
	SubStep      int
	Gravity      int
	Rounds       int
}

// active 返回每帧实际收到指令的实体数。
func (c Config) active() int {
	if c.ActiveBodies <= 0 || c.ActiveBodies > c.Bodies {
		return c.Bodies
	}
	return c.ActiveBodies
}

// Tier 是一档配置的完整结果。
type Tier struct {
	Config      Config
	FullHashes  []uint64
	IncHashes   []uint64
	Compare     hash.BitResult
	Equivalence EquivalenceSummary

	FullTotal   time.Duration // 端到端全量总耗时，中位数
	IncTotal    time.Duration // 端到端增量总耗时，中位数
	FullSamples []time.Duration
	IncSamples  []time.Duration

	AvgDirtyPerFrame float64 // 每帧平均真正重算的槽位数，解释增量收益大小

	FullHashPerFrame   time.Duration // 单帧全量指纹
	IncHashPerFrame1   time.Duration // 单帧增量指纹，1 个脏槽位
	IncHashPerFrameAll time.Duration // 单帧增量指纹，全部槽位都脏
}

// EquivalenceSummary 汇总逐帧 Check 的三类差异数。
type EquivalenceSummary struct {
	SlotDiffs  int // 缓存 D_i 与重算 D_i 不一致的槽位总数
	SumDiffs   int // 部分和不一致的帧数
	HashDiffs  int // 终值不一致的帧数
	BrokenSets []int
}

// Equal 报告逐帧 Check 是否全部逐位等价。
func (e EquivalenceSummary) Equal() bool {
	return e.SlotDiffs == 0 && e.SumDiffs == 0 && e.HashDiffs == 0
}

// TotalDiffs 返回三类差异数与逐位差值之和。
func (t Tier) TotalDiffs() int {
	return t.Equivalence.SlotDiffs + t.Equivalence.SumDiffs +
		t.Equivalence.HashDiffs + t.Compare.DiffBits
}

// BitIdentical 报告两条序列是否逐位相同（差值恒为零）。
func (t Tier) BitIdentical() bool {
	return t.Compare.DiffBits == 0 && t.Compare.FirstDiffAt < 0 &&
		len(t.FullHashes) == len(t.IncHashes)
}

// Verdict 返回等价判定文本。
func (t Tier) Verdict() string {
	if t.BitIdentical() && t.Equivalence.Equal() {
		return "逐位相同"
	}
	return "不等价"
}

// FullMinusInc 返回端到端全量总耗时减增量总耗时。
func (t Tier) FullMinusInc() time.Duration { return t.FullTotal - t.IncTotal }

// E2ERatio 返回端到端全量总耗时与增量总耗时之比。
func (t Tier) E2ERatio() float64 { return ratio(t.FullTotal, t.IncTotal) }

// HashPerFrameDiff 返回单帧指纹耗时差：全量 − 增量（1 个脏槽位）。
func (t Tier) HashPerFrameDiff() time.Duration { return t.FullHashPerFrame - t.IncHashPerFrame1 }

// HashRatio 返回单帧指纹耗时比（1 个脏槽位）。
func (t Tier) HashRatio() float64 { return ratio(t.FullHashPerFrame, t.IncHashPerFrame1) }

func ratio(a, b time.Duration) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Run 采集序列、逐帧对拍，并测端到端与单帧指纹两类耗时。
func Run(cfg Config) (Tier, error) {
	t := Tier{Config: cfg}
	frames := BuildFrames(cfg)

	_, fullSeq, err := replay.Record(
		api.Options{Bodies: cfg.Bodies, SubSteps: cfg.SubStep, Gravity: cfg.Gravity}, frames)
	if err != nil {
		return t, err
	}
	incSeq, eqs, err := recordIncremental(cfg, frames)
	if err != nil {
		return t, err
	}
	t.FullHashes, t.IncHashes = fullSeq, incSeq
	t.AvgDirtyPerFrame = avgDirty(cfg, frames)
	t.Compare = hash.CompareSequences(fullSeq, incSeq)
	for _, e := range eqs {
		t.Equivalence.SlotDiffs += e.SlotDiffs
		t.Equivalence.SumDiffs += e.ProductDiff
		t.Equivalence.HashDiffs += e.HashDiff
		t.Equivalence.BrokenSets = append(t.Equivalence.BrokenSets, e.BrokenSlots...)
	}

	t.FullTotal, t.FullSamples = medianRun(cfg.Rounds, func() time.Duration { return timeFull(cfg, frames) })
	t.IncTotal, t.IncSamples = medianRun(cfg.Rounds, func() time.Duration { return timeInc(cfg, frames) })
	t.FullHashPerFrame = measureFullHash(cfg.Bodies)
	t.IncHashPerFrame1 = measureIncHash(cfg.Bodies, 1)
	t.IncHashPerFrameAll = measureIncHash(cfg.Bodies, cfg.Bodies)
	return t, nil
}

// measureFullHash 测量一次全量指纹，不含仿真与状态构造。
func measureFullHash(bodies int) time.Duration {
	s := hashBenchState(bodies)
	return perFrameOp(func(i int) uint64 { return hash.State(i%97, s) })
}

// measureIncHash 测量一次增量更新。dirty 为每帧变化的槽位数。
func measureIncHash(bodies, dirty int) time.Duration {
	s := hashBenchState(bodies)
	inc := hash.NewIncremental(s)
	if dirty > bodies {
		dirty = bodies
	}
	if dirty < 1 {
		dirty = 1
	}
	return perFrameOp(func(i int) uint64 {
		s.ClearDirty()
		for k := 0; k < dirty; k++ {
			s.Push((i+k)%bodies, fixed.FromRaw(1), 0)
		}
		inc.SetTick(i % 97)
		return inc.Update()
	})
}

// medianRun 跑 rounds 轮取中位数，同时返回全部样本。
func medianRun(rounds int, fn func() time.Duration) (time.Duration, []time.Duration) {
	if rounds < 1 {
		rounds = 1
	}
	s := make([]time.Duration, rounds)
	for i := range s {
		s[i] = fn()
	}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2], s
}

func timeFull(cfg Config, frames []replay.Frame) time.Duration {
	w := mustWorld(cfg)
	start := time.Now()
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			panic(err)
		}
		_ = w.Hash()
	}
	return time.Since(start)
}

func timeInc(cfg Config, frames []replay.Frame) time.Duration {
	w := mustWorld(cfg)
	start := time.Now()
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			panic(err)
		}
		_ = w.HashIncremental()
	}
	return time.Since(start)
}

func mustWorld(cfg Config) *api.World {
	w, err := api.New(api.Options{Bodies: cfg.Bodies, SubSteps: cfg.SubStep, Gravity: cfg.Gravity})
	if err != nil {
		panic(err)
	}
	return w
}

// avgDirty 返回增量模式每帧平均重算的槽位数。
func avgDirty(cfg Config, frames []replay.Frame) float64 {
	w := mustWorld(cfg)
	total := 0
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			panic(err)
		}
		_ = w.HashIncremental()
		total += w.DirtyCount()
	}
	if len(frames) == 0 {
		return 0
	}
	return float64(total) / float64(len(frames))
}

// recordIncremental 跑一遍增量世界，返回每帧指纹与每帧对拍结果。
func recordIncremental(cfg Config, frames []replay.Frame) ([]uint64, []hash.Equivalence, error) {
	w := mustWorld(cfg)
	hashes := make([]uint64, 0, len(frames))
	eqs := make([]hash.Equivalence, 0, len(frames))
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, w.HashIncremental())
		eqs = append(eqs, w.Check())
	}
	return hashes, eqs, nil
}

// BuildFrames 构造确定性的指令序列：每帧给 active 个实体中的一个施加冲量。
func BuildFrames(cfg Config) []replay.Frame {
	active := cfg.active()
	frames := make([]replay.Frame, cfg.Frames)
	for i := range frames {
		frames[i] = replay.Frame{Commands: []api.Command{{
			Tick:  i,
			Body:  i % active,
			PushX: (1 << 13) + (i%7)<<10,
			PushY: -((1 << 12) + (i%5)<<11),
		}}}
	}
	return frames
}

// hashBenchState 构造一档实体数的状态，数值互不相同，避免退化。
func hashBenchState(n int) *sim.State {
	s := sim.NewState(n, 0)
	for i := 0; i < n; i++ {
		s.Push(i, fixed.FromRaw(i*7919), fixed.FromRaw(-i*104729))
	}
	return s
}

// perFrameOp 测量一次操作的耗时：预热后反复执行至少 20ms，取平均。
func perFrameOp(fn func(i int) uint64) time.Duration {
	const warm = 1000
	for i := 0; i < warm; i++ {
		_ = fn(i)
	}
	const minDur = 20 * time.Millisecond
	start := time.Now()
	n := 0
	var sink uint64
	for time.Since(start) < minDur {
		sink += fn(n)
		n++
	}
	_ = sink
	return time.Since(start) / time.Duration(n)
}

// WriteSequence 把一版指纹序列写成单独的存档文件。kind 为 "full" 或 "inc"。
func WriteSequence(w io.Writer, tier Tier, kind string) error {
	var hashes []uint64
	switch kind {
	case "full":
		hashes = tier.FullHashes
	case "inc":
		hashes = tier.IncHashes
	default:
		return fmt.Errorf("未知序列类型 %q", kind)
	}
	if _, err := fmt.Fprintf(w, "# label=%s bodies=%d frames=%d substeps=%d gravity=%d version=%s\n",
		tier.Config.Label, tier.Config.Bodies, tier.Config.Frames,
		tier.Config.SubStep, tier.Config.Gravity, kind); err != nil {
		return err
	}
	for i, h := range hashes {
		if _, err := fmt.Fprintf(w, "%d %016x\n", i, h); err != nil {
			return err
		}
	}
	return nil
}

// WriteCompare 输出两版序列的逐帧逐位比对，xor 列全为零即等价。
func WriteCompare(w io.Writer, tier Tier) error {
	if _, err := fmt.Fprintf(w, "# label=%s bodies=%d frames=%d substeps=%d gravity=%d xor=full^inc\n",
		tier.Config.Label, tier.Config.Bodies, tier.Config.Frames,
		tier.Config.SubStep, tier.Config.Gravity); err != nil {
		return err
	}
	for i := range tier.FullHashes {
		inc := uint64(0)
		if i < len(tier.IncHashes) {
			inc = tier.IncHashes[i]
		}
		if _, err := fmt.Fprintf(w, "%d full=%016x inc=%016x xor=%016x\n",
			i, tier.FullHashes[i], inc, tier.FullHashes[i]^inc); err != nil {
			return err
		}
	}
	return nil
}

func tableHeader(b *strings.Builder, cols ...string) {
	b.WriteString("| " + strings.Join(cols, " | ") + " |\n|")
	for range cols {
		b.WriteString("---|")
	}
	b.WriteString("\n")
}

// WriteBitCheck 输出逐位比对结论：差值恒为零才算等价。
func WriteBitCheck(w io.Writer, tiers []Tier) error {
	var b strings.Builder
	tableHeader(&b, "负载", "实体数", "帧数", "不同位总数", "有差异的帧数", "首个差异帧")
	for _, t := range tiers {
		first := "-"
		if t.Compare.FirstDiffAt >= 0 {
			first = fmt.Sprintf("%d", t.Compare.FirstDiffAt)
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %s |\n",
			t.Config.Label, t.Config.Bodies, t.Compare.Frames,
			t.Compare.DiffBits, t.Compare.DiffFrames, first)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteSummary 输出三栏主表：耗时、等价判定、差异数。
func WriteSummary(w io.Writer, tiers []Tier) error {
	var b strings.Builder
	tableHeader(&b,
		"负载", "实体数", "平均脏槽位/帧", "等价判定", "差异数",
		"全量端到端", "增量端到端", "端到端差", "端到端比值",
		"全量单帧指纹", "增量单帧指纹")
	for _, t := range tiers {
		fmt.Fprintf(&b, "| %s | %d | %.1f | %s | %d | %s | %s | %s | %.2fx | %s | %s |\n",
			t.Config.Label, t.Config.Bodies, t.AvgDirtyPerFrame, t.Verdict(), t.TotalDiffs(),
			msDur(t.FullTotal), msDur(t.IncTotal), msDur(t.FullMinusInc()), t.E2ERatio(),
			nsDur(t.FullHashPerFrame), nsDur(t.IncHashPerFrame1))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteHashTrend 按负载分组，报告单帧指纹耗时随实体数的变化。
func WriteHashTrend(w io.Writer, tiers []Tier) error {
	var b strings.Builder
	b.WriteString("单帧指纹耗时随实体数的变化（增量按每帧 1 个脏槽位计）：\n\n")
	labels := tierLabels(tiers)
	for _, label := range labels {
		fmt.Fprintf(&b, "### %s\n\n", label)
		tableHeader(&b, "实体数", "全量单帧指纹", "增量单帧指纹", "差值", "比值")
		for _, t := range tiers {
			if t.Config.Label != label {
				continue
			}
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %.2fx |\n",
				t.Config.Bodies, nsDur(t.FullHashPerFrame), nsDur(t.IncHashPerFrame1),
				nsDur(t.HashPerFrameDiff()), t.HashRatio())
		}
		b.WriteString("\n")
	}
	b.WriteString("实体少时全量一次只有几百纳秒，增量省下的绝对值也小，放进整帧仿真里几乎看不出来；")
	b.WriteString("实体数上去以后全量按实体数线性增长，增量仍几乎不随实体数变化，差距才拉开到三个数量级。\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// tierLabels 按首次出现顺序返回负载标签。
func tierLabels(tiers []Tier) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tiers {
		if !seen[t.Config.Label] {
			seen[t.Config.Label] = true
			out = append(out, t.Config.Label)
		}
	}
	return out
}

// WriteDirtySensitivity 输出增量对脏槽位数的敏感度。
func WriteDirtySensitivity(w io.Writer, tiers []Tier) error {
	var b strings.Builder
	b.WriteString("这一节按实体数统计，与负载无关（增量开销只取决于脏槽位数）：\n\n")
	tableHeader(&b, "实体数", "全量单帧", "增量(1脏)", "增量(全脏)", "全脏/全量")
	seen := map[int]bool{}
	for _, t := range tiers {
		if seen[t.Config.Bodies] {
			continue
		}
		seen[t.Config.Bodies] = true
		allRatio := ratio(t.IncHashPerFrameAll, t.FullHashPerFrame)
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %.2fx |\n",
			t.Config.Bodies, nsDur(t.FullHashPerFrame),
			nsDur(t.IncHashPerFrame1), nsDur(t.IncHashPerFrameAll), allRatio)
	}
	b.WriteString("\n每帧脏槽位数等于实体数时（全局重力），增量也要重算每个槽位，")
	b.WriteString("再叠加脏标记维护，反而比全量略慢；只有变化稀疏时才划算。\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func nsDur(d time.Duration) string { return fmt.Sprintf("%.1fns", float64(d)) }

func msDur(d time.Duration) string {
	v := float64(d) / float64(time.Millisecond)
	if v < 0 {
		return fmt.Sprintf("-%.3fms", -v)
	}
	return fmt.Sprintf("%.3fms", v)
}
