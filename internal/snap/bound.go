package snap

// 定宽二进制编码的字节常量。所有产物的“体积”都以这套规范编码为准，
// 这样体积是字段数 N 与改动量 k 的精确函数，上界可以直接算出来。
//
// 全量快照 Full：
//
//	magic "GFF1"                 4 B
//	uint16 N                     2 B
//	int32  tick                  4 B
//	N × int32 值                 4N B
//	合计 FullSize(N) = 10 + 4N
//
// 差分快照 Delta（每条记录就是“编号 + 旧值”）：
//
//	magic "GFD1"                 4 B
//	uint16 N                     2 B
//	int32  tick_before           4 B
//	uint16 k（条目数）            2 B
//	k × (uint16 编号 + int32 旧值) = 6k B
//	合计 DeltaSize(N,k) = 12 + 6k
const (
	MagicFull  = "GFF1"
	MagicDelta = "GFD1"

	MagicSize       = 4
	U16Size         = 2
	I32Size         = 4
	FullHeaderSize  = MagicSize + U16Size + I32Size           // 10
	DeltaHeaderSize = MagicSize + U16Size + I32Size + U16Size // 12
	IDSize          = U16Size                                 // 2
	EntrySize       = IDSize + I32Size                        // 6
	FieldValueSize  = I32Size                                 // 4
)

// FullSize 是 N 个字段的全量快照的精确字节数。
func FullSize(n int) int { return FullHeaderSize + FieldValueSize*n }

// DeltaSize 是 k 个改动字段的差分快照的精确字节数（定宽编码，故既是实际值也是上界）。
func DeltaSize(_ int, k int) int { return DeltaHeaderSize + EntrySize*k }

// DeltaSizeBound 是给定改动量 k 时差分体积的上界。
// 本实现为定宽编码，上界可取到等号；换变长编码时该式仍是保守上界。
func DeltaSizeBound(_ int, k int) int { return DeltaHeaderSize + EntrySize*k }

// RatioUpperBound 是“差分体积 / 全量体积”对所有合法改动量 0≤k≤N 的统一上界。
//
//	r(k) = (12 + 6k) / (10 + 4N)
//
// 关于 k 单调递增，因此最大值在 k=N（所有字段都变）处取到：
//
//	B(N) = (12 + 6N) / (10 + 4N)
//
// 极限 N→∞ 时 B→6/4 = 1.5：差分每条要额外付 2 字节编号，
// 所以极端情况下差分最多比全量大 50%，这是诚实的最坏情况。
func RatioUpperBound(n int) float64 {
	return float64(DeltaHeaderSize+EntrySize*n) / float64(FullSize(n))
}

// RatioFor 给出改动量 k 时的实际比值（在定宽编码下即该档上界比值）。
func RatioFor(n, k int) float64 {
	return float64(DeltaSize(n, k)) / float64(FullSize(n))
}

// RatioBoundAtK 是给定档位 k 的比值上界 r̄(k)=(12+6k)/(10+4N)。
func RatioBoundAtK(n, k int) float64 {
	return float64(DeltaSizeBound(n, k)) / float64(FullSize(n))
}

// DeltaSmallerThanFullThreshold 给出“差分严格小于全量”允许的最大改动量：
// 12+6k < 10+4N  ⇔  k < (4N-2)/6 = (2N-1)/3。
// 返回满足条件的最大整数 k。
func DeltaSmallerThanFullThreshold(n int) int {
	// 求最大 k 使 12+6k < 10+4N，即 6k < 4N-2。
	k := 0
	for 6*(k+1) < 4*n-2 {
		k++
	}
	return k
}
