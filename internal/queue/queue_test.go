package queue

import "testing"

// drainFrame 跑一个完整帧：BeginFrame -> 入队 -> Drain，返回保留的 value 与累计统计。
func drainFrame(q *Queue, cap int, values []int, weights []int64) ([]int, int, int64) {
	q.BeginFrame(cap)
	for i, v := range values {
		q.Enqueue(v, weights[i])
	}
	out := q.Drain(cap)
	return out, q.DropCount(), q.DropSum()
}

func TestTimingsEquivalent(t *testing.T) {
	// 同一保留规则（TopK）下，两种丢弃时机必须得到完全相同的保留集合，
	// 且输出顺序（按到达序）也一致。
	frames := [][]int64{
		{3, 1, 4, 1, 5, 9, 2, 6},
		{5, 5, 5, 2, 2, 7},       // 含同权重决胜
		{10, 20, 30},             // 未超容量
		{8, 8, 8, 8, 8, 8, 8, 8}, // 全部同权
	}
	for _, cap := range []int{1, 2, 3, 5} {
		imm := New(Immediate, TopK)
		def := New(Deferred, TopK)
		for fi, w := range frames {
			vals := make([]int, len(w))
			for i := range vals {
				vals[i] = fi*100 + i
			}
			gotI, dcI, sumI := drainFrame(imm, cap, vals, w)
			gotD, dcD, sumD := drainFrame(def, cap, vals, w)
			if !equalInts(gotI, gotD) {
				t.Fatalf("cap=%d frame=%d 保留集合随时机变化: immediate=%v deferred=%v", cap, fi, gotI, gotD)
			}
			if dcI != dcD || sumI != sumD {
				t.Fatalf("cap=%d frame=%d 丢弃统计随时机变化: (%d,%d) vs (%d,%d)", cap, fi, dcI, sumI, dcD, sumD)
			}
		}
	}
}

func TestTopKKeepsLargest(t *testing.T) {
	weights := []int64{3, 1, 4, 1, 5, 9, 2, 6}
	values := []int{0, 1, 2, 3, 4, 5, 6, 7}
	for _, cap := range []int{1, 2, 3, 4, 8} {
		for _, timing := range []Timing{Immediate, Deferred} {
			q := New(timing, TopK)
			got, _, sum := drainFrame(q, cap, values, weights)
			if len(got) != cap {
				t.Fatalf("cap=%d 保留数=%d", cap, len(got))
			}
			// 权重降序为 9,6,5,4,3,2,1,1；cap=4 的最优四条 value=5,7,4,2。
			want := map[int]bool{5: true, 7: true, 4: true, 2: true}
			for _, v := range got {
				if cap <= 4 && !want[v] {
					t.Fatalf("cap=%d timing=%v 错误保留了 value=%d", cap, timing, v)
				}
			}
			if got, want := sum, TailSum(weights, cap); got != want {
				t.Fatalf("cap=%d 丢弃和=%d 期望=%d", cap, got, want)
			}
		}
	}
}

func TestTailDropDropsNew(t *testing.T) {
	weights := []int64{3, 1, 4, 1, 5}
	values := []int{0, 1, 2, 3, 4}
	for _, timing := range []Timing{Immediate, Deferred} {
		q := New(timing, TailDrop)
		got, dc, _ := drainFrame(q, 2, values, weights)
		// 容量 2：只保留最早到达的两条（value 即到达序号）。
		if !equalInts(got, []int{0, 1}) {
			t.Fatalf("TailDrop timing=%v 保留=%v, 期望 [0 1]", timing, got)
		}
		if dc != 3 {
			t.Fatalf("TailDrop 丢弃数=%d", dc)
		}
	}
}

func TestZeroCapDropsAll(t *testing.T) {
	for _, timing := range []Timing{Immediate, Deferred} {
		q := New(timing, TopK)
		got, dc, sum := drainFrame(q, 0, []int{0, 1, 2}, []int64{3, 1, 4})
		if len(got) != 0 || dc != 3 || sum != 8 {
			t.Fatalf("timing=%v 零容量行为异常: got=%v dc=%d sum=%d", timing, got, dc, sum)
		}
	}
}

func TestMinCapacityBudget(t *testing.T) {
	weights := []int64{9, 6, 5, 4, 3, 2, 1, 1}
	// budget=0：任何丢弃都会产生正权重和，必须全保留 K=8。
	if k := MinCapacity(weights, 0); k != 8 {
		t.Fatalf("budget=0 -> K=%d, 期望 8", k)
	}
	if k := MinCapacity(weights, 1); k != 7 {
		t.Fatalf("budget=1 -> K=%d, 期望 7（丢最小的 1，和为 1）", k)
	}
	if k := MinCapacity(weights, 2); k != 6 {
		t.Fatalf("budget=2 -> K=%d, 期望 6（丢 1+1=2）", k)
	}
	if k := MinCapacity(weights, 3); k != 6 {
		t.Fatalf("budget=3 -> K=%d, 期望 6（再丢 2 则和为 4 > 3）", k)
	}
	if k := MinCapacity(weights, 100); k != 0 {
		t.Fatalf("budget 充足 -> K=%d, 期望 0", k)
	}
	// 关系自洽：对每个 budget，K(budget) 对应的 TopK 丢弃和必须 <= budget，
	// 而 K-1 对应的丢弃和必须 > budget（边界性质）。
	for _, budget := range []int64{0, 1, 2, 3, 4, 10, 30} {
		k := MinCapacity(weights, budget)
		if got := TailSum(weights, k); got > budget {
			t.Fatalf("budget=%d K=%d 丢弃和=%d 超预算", budget, k, got)
		}
		if k > 0 && TailSum(weights, k-1) <= budget {
			t.Fatalf("budget=%d K=%d 非最小：K-1 已满足预算", budget, k)
		}
	}
}

func TestStatsAccumulateAcrossFrames(t *testing.T) {
	q := New(Immediate, TopK)
	drainFrame(q, 2, []int{0, 1, 2}, []int64{3, 1, 4})       // 丢 1
	drainFrame(q, 2, []int{0, 1, 2, 3}, []int64{9, 8, 7, 6}) // 丢 6+7=13
	if q.DropCount() != 3 || q.DropSum() != 14 {
		t.Fatalf("跨帧统计错误: drops=%d sum=%d", q.DropCount(), q.DropSum())
	}
}

func equalInts(a, b []int) bool {
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
