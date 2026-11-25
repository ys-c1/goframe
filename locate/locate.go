// Package locate 在两条逐帧指纹序列之间定位首个分歧帧。
//
// 设两条确定性回放分别产生指纹序列 A[0..N)、B[0..N)，帧号逐帧对齐，
// 定义谓词 P(i) = (A[i] != B[i])。
//
// 定理一：首次哈希不同必定出现在首个状态不同的帧。
//
//	引理 1（指纹是状态的纯函数）：hash.State 只依赖帧号 tick 与 state.Raw()
//	导出的定长字节序列，不含任何外部随机性。因此两条回放在第 i 帧的 tick 与
//	状态字节相同时，指纹必然相同（internal/hash 的 TestHashStable 覆盖此性质）。
//	取逆否：第 i 帧指纹不同，则该帧 tick 或状态字节必有不同；帧号逐帧对齐，
//	tick 不可能不同，故状态必定不同。
//
//	引理 2（相等前缀 / 谓词单调）：锁步内核的 Step 是 (当前状态, 本帧指令)
//	的纯函数。两条回放输入同一份指令日志，若第 i 帧状态相同，则第 i+1 帧状态
//	也相同。归纳可知：任意分歧帧之前的所有帧状态全部相同，结合引理 1，其指纹
//	也全部相同。于是 P 必然形如 false…false、true…true，即“相等前缀 + 分歧
//	后缀”，这正是二分查找所要求的单调谓词。
//
//	记 s 为首个状态不同帧、h 为首个哈希不同帧。由引理 1 的逆否得 h >= s；
//	在第 s 帧状态不同，哈希无碰撞时指纹不同，得 h <= s。两式合并：h = s。
//
//	碰撞说明：FNV-1a 64 位指纹并非单射，严格的 h <= s 依赖无碰撞假设；工程上
//	64 位空间碰撞概率可忽略，且 api.World.Snapshot 提供逐字节状态导出，可在
//	定位帧上做兜底校验（见 locate 包测试中的快照断言）。
//
// 定理二：二分定位的比较次数上界为帧数的对数。
//
//	二分维护不变式：首个分歧帧始终落在区间 [lo, hi) 内，且 lo 之前的帧全部
//	相等。每次哈希比较把长度为 L 的区间缩为 ceil(L/2)，k 次比较后区间长度不
//	超过 ceil(N/2^k)，区间收缩为空即终止。因此比较次数
//		C <= ceil(log2(N+1)) = bits.Len(N)（N >= 1）。
//	作为对照，线性扫描最坏需要 N 次比较。N=100 时二分至多 7 次、N=1000 时至
//	多 10 次，而线性扫描分别至多 100、1000 次。
package locate

import "math/bits"

// Result 是定位结果。
type Result struct {
	// Frame 是首个分歧帧号（从 0 起）；两条序列完全一致时为 -1。
	Frame int
	// Comparisons 是定位过程中实际发生的哈希比较次数。
	Comparisons int
}

// commonLen 返回两条序列可比较的公共长度。
func commonLen(a, b []uint64) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	return n
}

// Linear 线性扫描：从第 0 帧起逐帧比较哈希，遇到首个不同帧即返回。
// 最坏比较 N 次；分歧落在第 d 帧时恰好比较 d+1 次。
func Linear(a, b []uint64) Result {
	n := commonLen(a, b)
	cmp := 0
	for i := 0; i < n; i++ {
		cmp++
		if a[i] != b[i] {
			return Result{Frame: i, Comparisons: cmp}
		}
	}
	// 公共前缀全部相等：长度不一致时分歧落在越界处，否则无分歧。
	if len(a) != len(b) {
		return Result{Frame: n, Comparisons: cmp}
	}
	return Result{Frame: -1, Comparisons: cmp}
}

// Binary 二分查找首个满足 A[i] != B[i] 的帧号，即单调谓词 P 的 lower_bound。
// 正确性依赖定理一的引理 2（相等前缀性质）：中点哈希相等即可安全丢弃左半边，
// 中点哈希不同即可把搜索范围收窄到中点及其左侧。
func Binary(a, b []uint64) Result {
	n := commonLen(a, b)
	lo, hi := 0, n
	cmp := 0
	for lo < hi {
		// 避免 lo+hi 溢出，并保证中点落在区间内。
		mid := int(uint(lo+hi) >> 1)
		cmp++
		if a[mid] != b[mid] {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if lo == n && len(a) == len(b) {
		return Result{Frame: -1, Comparisons: cmp}
	}
	return Result{Frame: lo, Comparisons: cmp}
}

// Bound 返回 N 帧序列上二分比较次数的紧上界 ceil(log2(N+1))。
// N 为 0 时返回 0。
func Bound(N int) int {
	if N <= 0 {
		return 0
	}
	return bits.Len(uint(N))
}
