package parallel

import (
	"errors"
	"sync"

	"goframe/internal/fixed"
)

// ErrSharedWrite 表示计划在给定分区下存在共享写入，Parallel 拒绝执行。
var ErrSharedWrite = errors.New("写入集合互不相交条件不成立：存在跨 worker 共享写入")

// MaxDelta 统计一次完整运行中所有操作实际增量绝对值的最大值 M。
// 所有操作都是定点加法且增量有界（冲量、重力、速度派生的耦合、速度本身），
// M 是差异上界 D <= 2·M·S·L 的常数因子之一。
func MaxDelta(state *State, plans []FramePlan) int64 {
	var m int64
	// 不污染真实状态：在克隆上重放一遍，保证 Compute 类操作取到与运行时同序的值。
	clone := state.Clone()
	for _, plan := range plans {
		for _, phase := range plan.Phases {
			for _, op := range phase {
				d := op.delta(clone).Raw()
				if d < 0 {
					d = -d
				}
				if int64(d) > m {
					m = int64(d)
				}
				clone.apply(op)
			}
		}
	}
	return m
}

// RunSerial 串行执行：帧内逐阶段、阶段内按切片顺序应用操作。
// 这是对拍的唯一基准；plans 必须由对应 Builder 在每帧前基于当前状态生成。
func RunSerial(state *State, inputs []FrameInput, build func(*State, FrameInput) FramePlan) []FramePlan {
	plans := make([]FramePlan, len(inputs))
	for t, in := range inputs {
		plan := build(state, in)
		plans[t] = plan
		for _, phase := range plan.Phases {
			for _, op := range phase {
				state.apply(op)
			}
		}
	}
	return plans
}

// RunInterleave 确定性交错执行，用“无冲突原子序 + 冲突位置双相轮”
// 稳定复现非原子读-改-写的后果。
//
// 真实协程代码里每个实体更新都是“读当前值 -> 计算 -> 写回”三步，不是原子加法。
// 本执行器按写入位置把阶段内操作分成两类：
//
//	无冲突位置：只被一个 worker（属主）写入，按计划原序原子执行，
//	           结果与串行完全一致；
//	冲突位置：被属主与其他 worker 共同写入。每个冲突位置独占一个双相轮：
//	   读半轮——所有写该位置的操作（按执行者 worker 编号、计划内顺序）
//	           在任何写回之前捕获同一当前值 base=state[Loc] 与各自增量 delta；
//	   写半轮——按相同顺序提交 state[Loc]=base+delta，后写者覆盖先写者，
//	           先写者的更新确定性丢失。
//
// 冲突轮按位置在计划中首次出现的顺序排列；每个共享写入操作都属于某个
// 冲突轮并独立贡献误差，因此末态距离随共享写入次数 S 增长。
// 整个调度完全确定、可复现，且不引入 Go 内存模型意义上的数据竞争。
// 阶段边界仍是全局屏障，交错只发生在阶段内部。
//
// workers==1 时不存在并发，退化为与 RunSerial 完全相同的原子顺序执行，
// 因此单 worker 档末态必然等于串行基准。
func RunInterleave(state *State, inputs []FrameInput, workers int, build func(*State, FrameInput) FramePlan) []FramePlan {
	type pending struct {
		loc   Loc
		base  fixed.Value
		delta fixed.Value
	}
	plans := make([]FramePlan, len(inputs))
	for t, in := range inputs {
		plan := build(state, in)
		plans[t] = plan
		for _, phase := range plan.Phases {
			if workers == 1 {
				for _, op := range phase {
					state.apply(op)
				}
				continue
			}
			// 按写入位置分组，并记录位置首次出现顺序以保证确定性。
			groups := make(map[Loc][]Op)
			var locOrder []Loc
			workerSeen := make(map[Loc]map[int]bool)
			for _, op := range phase {
				if _, ok := groups[op.Loc]; !ok {
					locOrder = append(locOrder, op.Loc)
					workerSeen[op.Loc] = make(map[int]bool)
				}
				groups[op.Loc] = append(groups[op.Loc], op)
				workerSeen[op.Loc][Owner(op.execBody(), workers, state.Len())] = true
			}
			for _, loc := range locOrder {
				ops := groups[loc]
				if len(workerSeen[loc]) == 1 {
					// 单一 worker 写入：原子顺序应用，与串行语义一致。
					for _, op := range ops {
						state.apply(op)
					}
					continue
				}
				// 多 worker 写入：双相轮。读半轮全部捕获后再统一写回。
				pend := make([]pending, 0, len(ops))
				for _, op := range ops {
					pend = append(pend, pending{
						loc:   loc,
						base:  state.get(loc),
						delta: op.delta(state),
					})
				}
				for _, p := range pend {
					state.set(p.loc, p.base.Add(p.delta))
				}
			}
		}
	}
	return plans
}

// RunParallel 使用真实 goroutine 并发执行，仅接受写入集合互不相交的计划。
// 每帧先构建计划并做条件判定；任何阶段存在共享写入立即返回 ErrSharedWrite，
// 不会产生带数据竞争的执行。阶段之间用 WaitGroup 屏障同步。
func RunParallel(state *State, inputs []FrameInput, workers int, build func(*State, FrameInput) FramePlan) ([]FramePlan, error) {
	n := state.Len()
	plans := make([]FramePlan, len(inputs))
	for t, in := range inputs {
		plan := build(state, in)
		plans[t] = plan
		for _, phase := range plan.Phases {
			if !phaseDisjoint(phase, workers, n) {
				return plans, ErrSharedWrite
			}
		}
		for _, phase := range plan.Phases {
			buckets := make([][]Op, workers)
			for _, op := range phase {
				w := Owner(op.execBody(), workers, n)
				buckets[w] = append(buckets[w], op)
			}
			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				if len(buckets[w]) == 0 {
					continue
				}
				wg.Add(1)
				go func(ops []Op) {
					defer wg.Done()
					for _, op := range ops {
						state.apply(op)
					}
				}(buckets[w])
			}
			wg.Wait() // 阶段屏障：下一阶段开始前所有 worker 可见本阶段全部写入
		}
	}
	return plans, nil
}

// phaseDisjoint 判定单阶段内每个字段位置是否只被其属主 worker 写入：
// 即每个操作的执行者 worker 必须等于写入位置的属主 worker。
func phaseDisjoint(phase []Op, workers, n int) bool {
	for _, op := range phase {
		if Owner(op.execBody(), workers, n) != Owner(op.Loc.Body, workers, n) {
			return false
		}
	}
	return true
}

// Distance 返回两个末态逐字段的 L1 原始单位距离 D 与取值不同的字段位置数。
// D 的每个分量是定点原始整数差的绝对值之和。
func Distance(a, b *State) (sum int64, diffLocs int) {
	ra, rb := a.Raw(), b.Raw()
	for i := range ra {
		d := int64(ra[i]) - int64(rb[i])
		if d != 0 {
			diffLocs++
		}
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum, diffLocs
}

// FNV 复刻 internal/hash 的混合顺序，对 parallel.State 直接取指纹，
// 避免 parallel 反向依赖 internal/hash（后者依赖 internal/sim）。
func stateHash(tick int, s *State) uint64 {
	const offset = 14695981039346656037
	const prime = 1099511628211
	h := uint64(offset)
	mix := func(v uint64) {
		h ^= v
		h *= prime
	}
	mix(uint64(uint32(tick)))
	for _, v := range s.Raw() {
		mix(uint64(uint32(v)))
	}
	return h
}

// Hash 返回帧号与状态的指纹，口径与 internal/hash.State 完全一致。
func Hash(tick int, s *State) uint64 { return stateHash(tick, s) }
