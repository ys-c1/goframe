package hash

// BitResult 是一次逐位比对的结论。
type BitResult struct {
	Frames      int
	DiffBits    int // 两条序列逐帧异或后 popcount 之和
	DiffFrames  int // 至少有一位不同的帧数
	FirstDiffAt int // 首个不同帧号，-1 表示全同
}

// CompareSequences 逐帧逐位比较两条指纹序列。差值恒为零才算等价。
func CompareSequences(full, inc []uint64) BitResult {
	n := len(full)
	if len(inc) < n {
		n = len(inc)
	}
	r := BitResult{Frames: n, FirstDiffAt: -1}
	for i := 0; i < n; i++ {
		d := full[i] ^ inc[i]
		if d == 0 {
			continue
		}
		if r.FirstDiffAt < 0 {
			r.FirstDiffAt = i
		}
		r.DiffFrames++
		r.DiffBits += popcount(d)
	}
	if len(full) != len(inc) && r.FirstDiffAt < 0 {
		r.FirstDiffAt = n
	}
	return r
}

// popcount 返回 64 位值的置位数。
func popcount(v uint64) int {
	n := 0
	for v != 0 {
		v &= v - 1
		n++
	}
	return n
}
