// Package queue 提供有界指令队列。
//
// 队列在容量不足时必须丢弃部分指令，支持两个正交维度：
//   - Timing：何时做丢弃决策（入队即丢 Immediate / 出队时再丢 Deferred）
//   - Policy：按什么规则保留（按冲量大小保留最大的 K 条 TopK / 丢新指令 TailDrop）
//
// 容量按帧给出：Enqueue 阶段以本帧容量 cap 在线维护（Immediate），
// 或先全部缓存、Drain(cap) 时再截断（Deferred）。两种时机在同一保留规则、
// 同一决胜次序下产出完全相同的保留集合（见 TestTimingsEquivalent）。
package queue

import "sort"

// Timing 决定丢弃决策发生的时机。
type Timing int

const (
	// Immediate 在入队的当下就决定保留/丢弃，任何时刻缓存不超过本帧容量。
	Immediate Timing = iota
	// Deferred 先缓存全部入队指令，到 Drain 时才按规则截断，
	// 用于验证“丢弃时机不影响最终保留集合（在同一保留规则下）”。
	Deferred
)

// Policy 决定容量不足时保留哪些指令。
type Policy int

const (
	// TopK 按权重（冲量大小）保留最大的 cap 条；权重相同则保留后到的。
	TopK Policy = iota
	// TailDrop 容量满时直接丢弃新到的指令，即旧实现的“丢新指令”策略。
	TailDrop
)

// item 是队列内部条目，seq 为帧内到达序号（每帧从 0 重新计），用于同权重决胜。
type item struct {
	value  int
	weight int64
	seq    int
}

// Queue 是有界指令队列，零值不可用，请用 New 构造。
type Queue struct {
	timing Timing
	policy Policy

	buf       []item // Immediate+TopK 下为堆（堆顶最差）；其余模式为顺序缓存
	frameCap  int    // 当前帧容量，由 BeginFrame 设定
	nextSeq   int
	dropCount int
	dropSum   int64 // 累计被丢指令权重（冲量大小）之和
}

// New 创建队列。容量在每帧 Drain 时给出；Immediate 模式下在 Enqueue 前须先 BeginFrame。
func New(timing Timing, policy Policy) *Queue {
	return &Queue{timing: timing, policy: policy}
}

// BeginFrame 开启一个新帧并声明本帧容量 cap。
// Immediate 模式据此在线截断；Deferred 模式仅记录容量，截断推迟到 Drain。
func (q *Queue) BeginFrame(cap int) {
	q.buf = q.buf[:0]
	q.nextSeq = 0
	q.frameCap = cap
}

// Enqueue 放入一条权重为 weight 的指令。weight 必须非负。
func (q *Queue) Enqueue(value int, weight int64) {
	it := item{value: value, weight: weight, seq: q.nextSeq}
	q.nextSeq++

	if q.frameCap <= 0 { // 容量 0：本帧全部丢弃
		q.dropCount++
		q.dropSum += weight
		return
	}

	if q.timing == Deferred {
		// 延迟决策：先全部收下，Drain 时再截断。
		q.buf = append(q.buf, it)
		return
	}

	// Immediate：入队即决策。
	if q.policy == TailDrop {
		if len(q.buf) < q.frameCap {
			q.buf = append(q.buf, it)
		} else {
			q.dropCount++
			q.dropSum += weight
		}
		return
	}

	// Immediate + TopK：维护“当前见过的指令里权重最大的 frameCap 条”。
	if len(q.buf) < q.frameCap {
		q.pushHeap(it)
		return
	}
	// 堆顶是已保留指令中保留优先级最低者；新指令严格更优才替换它。
	if better(it, q.buf[0]) {
		drop := q.buf[0]
		q.buf[0] = it
		q.fixRoot()
		q.dropCount++
		q.dropSum += drop.weight
	} else {
		q.dropCount++
		q.dropSum += weight
	}
}

// Drain 结束本帧并返回保留的全部指令（按到达顺序），统计信息跨帧累计。
// 参数 cap 对 Deferred 模式是截断容量；Immediate 模式下须与 BeginFrame 的容量一致。
func (q *Queue) Drain(cap int) []int {
	var out []int
	if q.timing == Immediate || cap <= 0 {
		items := append([]item(nil), q.buf...)
		sort.SliceStable(items, func(i, j int) bool { return items[i].seq < items[j].seq })
		out = make([]int, len(items))
		for i, it := range items {
			out[i] = it.value
		}
	} else {
		// Deferred：此刻才在完整到达集合上做截断。
		all := q.buf
		if len(all) > cap {
			var keep map[int]bool
			if q.policy == TopK {
				keep = selectTopK(all, cap)
			} else {
				keep = selectFirstK(all, cap) // TailDrop：保留最早到达的 cap 条
			}
			q.dropCount += len(all) - cap
			for _, it := range all {
				if !keep[it.seq] {
					q.dropSum += it.weight
				}
			}
			out = make([]int, 0, cap)
			// 按到达顺序输出，保证两种时机下指令的施加顺序也完全一致。
			for _, it := range all {
				if keep[it.seq] {
					out = append(out, it.value)
				}
			}
		} else {
			out = make([]int, len(all))
			for i, it := range all {
				out[i] = it.value
			}
		}
	}
	q.buf = q.buf[:0]
	q.frameCap = 0
	return out
}

// DropCount 返回累计丢弃指令数。
func (q *Queue) DropCount() int { return q.dropCount }

// DropSum 返回累计被丢指令权重（冲量大小）之和。
func (q *Queue) DropSum() int64 { return q.dropSum }

// selectTopK 返回保留优先级最高的 k 个条目的 seq 集合；同权重取后到者。
func selectTopK(items []item, k int) map[int]bool {
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	// 稳定排序、按优先级升序：最差在前，最优的 k 个在末尾。
	sort.SliceStable(idx, func(a, b int) bool {
		return worse(items[idx[a]], items[idx[b]])
	})
	keep := make(map[int]bool, k)
	for _, i := range idx[len(idx)-k:] {
		keep[items[i].seq] = true
	}
	return keep
}

// selectFirstK 返回最早到达的 k 个条目的 seq 集合（TailDrop 的延迟决策版）。
func selectFirstK(items []item, k int) map[int]bool {
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return items[idx[a]].seq < items[idx[b]].seq
	})
	keep := make(map[int]bool, k)
	for _, i := range idx[:k] {
		keep[items[i].seq] = true
	}
	return keep
}

// better 定义保留优先级：a 严格优于 b 当且仅当权重更大，
// 或权重相等而到达序号更大（同权重后到者优先）。
// 取“后到者优先”是因为 Immediate 模式做在线替换时，
// 这是唯一能与 Deferred 模式（在完整到达集合上选 TopK）得到同一保留集合的决胜规则。
func better(a, b item) bool {
	if a.weight != b.weight {
		return a.weight > b.weight
	}
	return a.seq > b.seq
}

// worse 是 better 的反向判定，用于堆序与排序。
func worse(a, b item) bool { return better(b, a) }

// pushHeap 把条目压入以 buf[0] 为堆顶的堆，堆顶始终是最差（优先级最低）条目。
func (q *Queue) pushHeap(it item) {
	q.buf = append(q.buf, it)
	i := len(q.buf) - 1
	for i > 0 {
		parent := (i - 1) / 2
		// 堆不变量：父节点更差、子节点更优。若父节点反而比新节点更优，交换让新节点上浮。
		if better(q.buf[parent], q.buf[i]) {
			q.buf[parent], q.buf[i] = q.buf[i], q.buf[parent]
			i = parent
		} else {
			break
		}
	}
}

// fixRoot 在替换堆顶后向下调整，恢复堆性质：堆顶始终是最差条目。
func (q *Queue) fixRoot() {
	n := len(q.buf)
	i := 0
	for {
		l, r, worst := 2*i+1, 2*i+2, i
		// 找出自身与两个子节点中最差（保留优先级最低）者；若子节点更差，堆顶下沉。
		if l < n && better(q.buf[worst], q.buf[l]) {
			worst = l
		}
		if r < n && better(q.buf[worst], q.buf[r]) {
			worst = r
		}
		if worst == i {
			return
		}
		q.buf[i], q.buf[worst] = q.buf[worst], q.buf[i]
		i = worst
	}
}

// MinCapacity 返回在权重序列 weights 上保证“被丢权重之和 <= budget”
// 所需的最小保留条数 K。全保留对应 K=len(weights)，此时丢弃和为零。
// budget < 0 时返回 len(weights)。
func MinCapacity(weights []int64, budget int64) int {
	if budget < 0 {
		return len(weights)
	}
	sorted := append([]int64(nil), weights...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })
	var tail int64 // 当前假设丢弃的最小若干项之和
	k := len(sorted)
	for k > 0 && tail+sorted[k-1] <= budget {
		tail += sorted[k-1]
		k--
	}
	return k
}

// TailSum 返回只保留权重最大的 k 条时，被丢权重之和（TopK 的丢弃和）。
func TailSum(weights []int64, k int) int64 {
	if k >= len(weights) {
		return 0
	}
	sorted := append([]int64(nil), weights...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })
	var sum int64
	for _, w := range sorted[k:] {
		sum += w
	}
	return sum
}
