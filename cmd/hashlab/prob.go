// Package main 的 prob.go 给出两种“窄位哈希”漏判概率的闭式函数，并用穷举对齐验证。
package main

// ---- 模型一：丢低位（分桶量化），桶宽 B=2^b，保留位宽 w=32-b ----
//
// 两个位置 x 与 x+Δ 被量化到同一桶，当且仅当 floor(x/B)==floor((x+Δ)/B)。
// 令对齐 r = x mod B 在 [0,B) 内均匀，并取 d=|Δ|：
//
//	d≥B 时两位置至少相隔一个完整桶，永远不可能同桶，漏判对齐数为 0；
//	d<B 时同桶条件为 r < B-d，B 个对齐里恰有 B-d 个漏判。
//
// 故漏判对齐数 M(d,b)=max(0, B-d)，漏判概率为位置差与哈希位宽的函数：
//
//	P_miss_drop(Δ,b) = max(0, 1 - |Δ|/B),   B = 2^b
//
// 注意它与“只取低位”模型不同：差一个完整桶宽（Δ=B）在这里必然可区分，
// 而不是必然漏判（见模型二）。

// missCountDropLow 返回在一个完整桶的 B 个对齐 r=0..B-1 上，量化后同桶的对齐数。
func missCountDropLow(delta, b int) int {
	B := 1 << b
	d := delta
	if d < 0 {
		d = -d
	}
	if d >= B {
		return 0
	}
	return B - d
}

// probDropLow 是分桶模型漏判概率的闭式函数，入参为位置差（raw）与丢弃低位位数 b。
func probDropLow(delta, b int) float64 {
	B := 1 << b
	return float64(missCountDropLow(delta, b)) / float64(B)
}

// ---- 模型二：字面“只取低位”，字段只保留低 k 位 ----
//
// (x & (2^k-1)) == ((x+Δ) & (2^k-1)) 当且仅当 2^k 整除 Δ，
// 与对齐 x 无关。故固定 Δ 时漏判是 0/1 确定事件：
//
//	P_miss_lowonly(Δ,k) = 1[ 2^k | Δ ]
//
// 若 Δ 在 1..D 上均匀随机，则 P ≈ floor(D/2^k)/D ≈ 2^-k。

// missCountKeepLow 在 B=2^k 个对齐上统计低 k 位相等的对齐数：要么全漏（B 个），要么 0。
func missCountKeepLow(delta, k int) int {
	B := 1 << k
	d := delta
	if d < 0 {
		d = -d
	}
	if d%B == 0 {
		return B
	}
	return 0
}

// probKeepLow 是只取低位模型的漏判概率（固定 Δ：0 或 1）。
func probKeepLow(delta, k int) float64 {
	B := 1 << k
	return float64(missCountKeepLow(delta, k)) / float64(B)
}

// empiricalMissDropLow 用真实 FNV 窄位哈希穷举 B 个对齐，统计“单字段差 Δ、其余字段相同”
// 的一对状态被判相等的次数。哈希在量化后字段上是确定性双射链，结果应与 missCountDropLow 恒等。
func empiricalMissDropLow(delta, b int) int {
	B := 1 << b
	h := dropLowHash(uint(b))
	miss := 0
	for r := 0; r < B; r++ {
		base := []int32{int32(r), 7, 9, 11} // 其余字段固定，仅 posX 差 Δ
		forked := []int32{int32(r + delta), 7, 9, 11}
		if h(0, base) == h(0, forked) {
			miss++
		}
	}
	return miss
}

// empiricalMissKeepLow 对“只取低位”模型做同样的穷举。
func empiricalMissKeepLow(delta, k int) int {
	B := 1 << k
	h := keepLowHash(uint(k))
	miss := 0
	for r := 0; r < B; r++ {
		base := []int32{int32(r), 7, 9, 11}
		forked := []int32{int32(r + delta), 7, 9, 11}
		if h(0, base) == h(0, forked) {
			miss++
		}
	}
	return miss
}

// growingFirstJudgeTick 按分桶模型解析计算增长差 Δ(t)=t·Δ（t 从首差帧起计 1,2,3,...）
// 首次跨桶的 t；窗口内不跨桶返回 -1。
func growingFirstJudgeTick(delta, b, window int) int {
	d := delta
	if d < 0 {
		d = -d
	}
	for t := 1; t <= window; t++ {
		// 增长差在首差帧后第 t 帧的位置差为 t·Δ；量化桶不同即窄位哈希可见。
		if quantize(0, b) != quantize(t*d, b) {
			return t
		}
	}
	return -1
}

// quantize 复现 dropLowHash 的字段量化：清掉最低 b 位。
func quantize(v, b int) int32 {
	return int32(uint32(v) >> uint(b) << uint(b))
}
