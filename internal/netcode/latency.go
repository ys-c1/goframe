// Package netcode 用离散延迟分布驱动回滚引擎，量化回滚深度与越界失败。
// 所有采样只用整数千分位与固定种子 LCG，跨机器逐位可复现。
package netcode

import (
	"fmt"

	"goframe/api"
	"goframe/internal/rollback"
)

// FrameMS 是帧长，50ms 对应 20Hz 确定性帧同步。
const FrameMS = 50

// Tier 是一档延迟分布：延迟以帧为单位，质量用千分位表示，总和必须为 1000。
type Tier struct {
	Name   string
	Seed   int64
	Frames int // 采样帧数（也是仿真时长）
	// DelayFrames[k] 与 MassPermille[k] 成对：延迟 k 帧的千分位概率。
	DelayFrames  []int
	MassPermille []int
}

// 三档分布的设计目标（帧长 50ms）：
//   LAN  ：p99=2 帧=100ms，最大 3 帧，D*=2，D=5 完全覆盖
//   4G   ：p99=6 帧=300ms，最大 8 帧，D*=6，D=5 时尾部落界
//   弱网 ：p99=12 帧=600ms，最大 14 帧，D*=12，D=5 大量越界
func DefaultTiers() []Tier {
	return []Tier{
		{
			Name:         "lan",
			Seed:         2026112601,
			Frames:       600,
			DelayFrames:  []int{1, 2, 3},
			MassPermille: []int{850, 140, 10}, // p99=2
		},
		{
			Name:   "4g",
			Seed:   2026112602,
			Frames: 600,
			// F: 1->.40 2->.70 3->.86 4->.94 5->.97 6->.99 7->.996 8->1
			DelayFrames:  []int{1, 2, 3, 4, 5, 6, 7, 8},
			MassPermille: []int{400, 300, 160, 80, 30, 20, 6, 4}, // p99=6
		},
		{
			Name:   "wan",
			Seed:   2026112603,
			Frames: 600,
			// F: 1..5=.65 6=.70 7=.75 8=.80 9=.85 10=.90 11=.95 12=.98 13=.99 14=1
			DelayFrames: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
			MassPermille: []int{
				250, 150, 100, 80, 70,
				50, 50, 50, 50, 50,
				50, 30, 10, 10,
			}, // p99=12
		},
	}
}

// lcg 是固定参数的线性同余生成器，确定性且无平台依赖。
type lcg struct{ state uint64 }

func newLCG(seed int64) *lcg { return &lcg{state: uint64(seed)} }

// nextPermille 返回 [0,999] 的整数。
func (g *lcg) nextPermille() int {
	// Numerical Recipes 常数。
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return int((g.state >> 33) % 1000)
}

// SampleTrace 生成固定长度的帧延迟轨迹（单位帧），同种子同分布逐位复现。
func (t Tier) SampleTrace() ([]int, error) {
	if len(t.DelayFrames) != len(t.MassPermille) {
		return nil, fmt.Errorf("分布 %s 帧数与质量长度不一致", t.Name)
	}
	sum := 0
	for _, m := range t.MassPermille {
		sum += m
	}
	if sum != 1000 {
		return nil, fmt.Errorf("分布 %s 千分位总和=%d，必须为 1000", t.Name, sum)
	}
	g := newLCG(t.Seed)
	out := make([]int, t.Frames)
	for i := range out {
		draw := g.nextPermille()
		cum := 0
		for k, m := range t.MassPermille {
			cum += m
			if draw < cum {
				out[i] = t.DelayFrames[k]
				break
			}
		}
		if out[i] == 0 {
			out[i] = t.DelayFrames[len(t.DelayFrames)-1]
		}
	}
	return out, nil
}

// Quantile 返回轨迹的经验分位延迟（帧）。
// 采用最近秩法：秩 k=ceil(q*n)（1 基），即 F^{-1}(q)=min{x: F(x) >= q}。
func Quantile(trace []int, q float64) int {
	counts := map[int]int{}
	max := 0
	for _, d := range trace {
		counts[d]++
		if d > max {
			max = d
		}
	}
	k := int(q * float64(len(trace)))
	if float64(k) < q*float64(len(trace)) {
		k++ // ceil
	}
	if k < 1 {
		k = 1
	}
	seen := 0
	for d := 0; d <= max; d++ {
		seen += counts[d]
		if seen >= k {
			return d
		}
	}
	return max
}

// FrameBytes 是单实体单帧快照字节数：4 个 Q16.16 字段，每字段 4 字节。
const FrameBytesPerBody = 16

// SnapshotBytes 返回 bodies 个实体一帧快照的有效载荷字节数。
func SnapshotBytes(bodies int) int { return FrameBytesPerBody * bodies }

// RequiredDepth 是理论所需深度：延迟分位向上取整到帧（分布本就离散，即分位帧值）。
func RequiredDepth(quantileDelayFrames int) int { return quantileDelayFrames }

// MaxDepth 返回给定内存预算（字节）下能保留的最大回滚深度。
// 环容量为 D+1 帧快照，故 D = floor(Budget / perFrame) - 1。
func MaxDepth(budgetBytes, bodies int) int {
	perFrame := SnapshotBytes(bodies)
	d := budgetBytes/perFrame - 1
	if d < 0 {
		return 0
	}
	return d
}

// TierResult 是一档实验的汇总指标。
type TierResult struct {
	Tier          Tier
	Trace         []int
	P50, P95, P99 int // 经验分位，帧
	MaxDelay      int
	TheoreticD    int
	Depth5        DepthRun
	DepthStar     DepthRun
}

// DepthRun 是固定深度下跑完整轨迹的结果。
type DepthRun struct {
	Depth        int
	Rollbacks    int // 窗口内成功回滚次数
	OOB          int // 越界失败次数
	FailedTicks  []int
	FirstOOBTick int // 第一次越界时的到达时刻 atTick
	FirstOOBTarget int
}

// RunTier 对一档分布分别在 D=5 与理论深度 D* 下驱动引擎。
func RunTier(t Tier, opt api.Options) (*TierResult, error) {
	trace, err := t.SampleTrace()
	if err != nil {
		return nil, err
	}
	res := &TierResult{Tier: t, Trace: trace}
	res.P50 = Quantile(trace, 0.50)
	res.P95 = Quantile(trace, 0.95)
	res.P99 = Quantile(trace, 0.99)
	for _, d := range trace {
		if d > res.MaxDelay {
			res.MaxDelay = d
		}
	}
	res.TheoreticD = RequiredDepth(res.P99)

	res.Depth5 = runDepth(t, trace, opt, 5)
	res.DepthStar = runDepth(t, trace, opt, res.TheoreticD)
	return res, nil
}

// runDepth 按真实时钟驱动引擎：每帧先处理在本帧到达的远端输入
// （atTick=target+delay），再推进本地时钟。窗口随推进逐帧滑动，
// 因此 atTick 时刻 target < atTick-depth 的输入必然越界。
func runDepth(t Tier, trace []int, opt api.Options, depth int) DepthRun {
	eng, err := rollback.New(opt, depth)
	if err != nil {
		panic(err)
	}
	n := len(trace)
	// 到达桶：arrivals[at] 是第 at 帧时钟开始时到达的全部远端输入。
	type arrival struct{ target, delay int }
	buckets := make([][]arrival, n+t.maxDelay()+1)
	for target, d := range trace {
		at := target + d
		buckets[at] = append(buckets[at], arrival{target, d})
	}
	run := DepthRun{Depth: depth, FirstOOBTick: -1, FirstOOBTarget: -1}
	for tick := 0; tick < n; tick++ {
		// 1) 处理本帧到达的远端输入，此时窗口覆盖 [tick-depth, tick-1] 的推进前快照。
		for _, a := range buckets[tick] {
			remote := []api.Command{{
				Tick: a.target, Body: (a.target + 1) % opt.Bodies,
				PushY: 1 << 13,
			}}
			err := eng.Inject(tick, a.target, remote, a.delay)
			if err != nil {
				run.OOB++
				run.FailedTicks = append(run.FailedTicks, a.target)
				if run.FirstOOBTick < 0 {
					run.FirstOOBTick = tick
					run.FirstOOBTarget = a.target
				}
				continue
			}
			run.Rollbacks++
		}
		// 2) 本地准时输入推进时钟，推进前快照随之入环、最老快照被驱逐。
		local := api.Command{Tick: tick, Body: tick % opt.Bodies, PushX: 1 << 14}
		if err := eng.Advance([]api.Command{local}); err != nil {
			panic(err)
		}
	}
	return run
}

func (t Tier) maxDelay() int {
	m := 0
	for _, d := range t.DelayFrames {
		if d > m {
			m = d
		}
	}
	return m
}

// MaxDelay 暴露分布定义中的最大延迟帧值。
func (t Tier) MaxDelay() int { return t.maxDelay() }
