package rng

import (
	"math"
	"testing"
)

const testMaster = uint64(0x20261126)

// TestDeriveStable 同一元组反复派生必须逐项相同。
func TestDeriveStable(t *testing.T) {
	for e := 0; e < 16; e++ {
		for c := uint64(0); c < 128; c++ {
			a := Derive(testMaster, e, TagJitterX, c)
			b := Derive(testMaster, e, TagJitterX, c)
			if a != b {
				t.Fatalf("派生不稳定 e=%d c=%d: %x != %x", e, c, a, b)
			}
		}
	}
}

// TestCallOrderIndependence 正序、逆序、打乱顺序计算，规范化后必须完全一致。
func TestCallOrderIndependence(t *testing.T) {
	const bodies, frames = 12, 64
	type key struct{ e, f int }
	keys := make([]key, 0, bodies*frames)
	for f := 0; f < frames; f++ {
		for e := 0; e < bodies; e++ {
			keys = append(keys, key{e, f})
		}
	}

	gen := func(order []key) []uint64 {
		// 按 (frame, entity) 规范存放，模拟输出序列。
		out := make([]uint64, bodies*frames)
		for _, k := range order {
			out[k.f*bodies+k.e] = Derive(testMaster, k.e, TagJitterX, uint64(k.f))
		}
		return out
	}

	forward := keys
	reverse := make([]key, len(keys))
	for i, k := range keys {
		reverse[len(keys)-1-i] = k
	}
	shuffled := append([]key(nil), keys...)
	// 确定性置换：用派生值本身决定交换，不依赖 math/rand。
	for i := range shuffled {
		j := int(Derive(testMaster, i, 0x5348, 7)%uint64(len(shuffled)))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	a, b, c := gen(forward), gen(reverse), gen(shuffled)
	for i := range a {
		if a[i] != b[i] || a[i] != c[i] {
			t.Fatalf("调用次序影响结果 at %d: %x %x %x", i, a[i], b[i], c[i])
		}
	}
}

// TestPurposeTagsSeparate X/Y 用途标签必须给出不同序列。
func TestPurposeTagsSeparate(t *testing.T) {
	sx := NewStream(testMaster, 3, TagJitterX)
	sy := NewStream(testMaster, 3, TagJitterY)
	same := 0
	for i := 0; i < 1024; i++ {
		if sx.Uint64() == sy.Uint64() {
			same++
		}
	}
	if same > 2 { // 偶然相等概率 ~2^-64/项，几乎不可能
		t.Fatalf("不同用途序列过度重合: %d", same)
	}
}

// TestEntityStreamsUncorrelated 用符号位汉明差异率与 Pearson 相关做宽松统计断言。
func TestEntityStreamsUncorrelated(t *testing.T) {
	const samples = 8192
	streams := make([][]float64, 6)
	bits := make([][]uint64, 6)
	for e := range streams {
		streams[e] = make([]float64, samples)
		bits[e] = make([]uint64, samples/64)
		for k := 0; k < samples; k++ {
			r := Derive(testMaster, e, TagJitterX, uint64(k))
			streams[e][k] = float64(r>>11) / float64(1<<53) // [0,1)
			if r&(1<<63) != 0 {
				bits[e][k/64] |= 1 << uint(k%64)
			}
		}
	}

	for e := 1; e < 6; e++ {
		// 汉明差异率应接近 0.5，给非常宽松的边界避免偶发 flaky。
		diff := 0
		for w := range bits[0] {
			diff += popcount(bits[0][w] ^ bits[e][w])
		}
		d := float64(diff) / samples
		if d < 0.45 || d > 0.55 {
			t.Fatalf("实体 0/%d 汉明差异率异常: %.4f", e, d)
		}
		if r := pearson(streams[0], streams[e]); math.Abs(r) > 0.08 {
			t.Fatalf("实体 0/%d 相关性过强: r=%.4f", e, r)
		}
	}
}

// TestGlobalSourceDependsOnSeedAndCalls 反证：共享全局源的目标取值
// 同时取决于种子与此之前的调用次数。
func TestGlobalSourceDependsOnSeedAndCalls(t *testing.T) {
	valueAt := func(seed int64, precedingCalls int) uint64 {
		g := NewSharedSource(seed)
		for i := 0; i < precedingCalls; i++ {
			g.Uint64()
		}
		return g.Uint64()
	}

	// 种子不同 -> 不同（极小概率撞值，固定种子下结果确定，若撞了说明选种不好）。
	if valueAt(1, 0) == valueAt(2, 0) {
		t.Fatal("不同种子首项竟相同")
	}
	// 同种子，调用位置（=此前调用次数）不同 -> 目标项不同。
	v0 := valueAt(42, 0)
	callCountChanges := false
	for k := 1; k <= 8; k++ {
		if valueAt(42, k) != v0 {
			callCountChanges = true
		}
	}
	if !callCountChanges {
		t.Fatal("调用次数没有改变目标取值，反证前提异常")
	}

	// 典型事故复现：实体 1 的序列被实体 0 的额外调用次数平移。
	entity1First := func(extraCallsByEntity0 int) uint64 {
		g := NewSharedSource(7)
		for i := 0; i < extraCallsByEntity0; i++ {
			g.Uint64() // 实体 0 本帧多抽的次数（例如多了一个随机分支）
		}
		return g.Uint64() // 实体 1 的第一次扰动
	}
	if entity1First(0) == entity1First(1) || entity1First(1) == entity1First(2) {
		t.Fatal("额外调用未平移其他实体的序列")
	}
}

func popcount(x uint64) int {
	n := 0
	for x != 0 {
		n += int(x & 1)
		x >>= 1
	}
	return n
}

func pearson(a, b []float64) float64 {
	var sa, sb float64
	for i := range a {
		sa += a[i]
		sb += b[i]
	}
	n := float64(len(a))
	ma, mb := sa/n, sb/n
	var cov, va, vb float64
	for i := range a {
		x, y := a[i]-ma, b[i]-mb
		cov += x * y
		va += x * x
		vb += y * y
	}
	if va == 0 || vb == 0 {
		return 0
	}
	return cov / math.Sqrt(va*vb)
}

func BenchmarkDerive(b *testing.B) {
	sink := uint64(0)
	for i := 0; i < b.N; i++ {
		sink ^= Derive(testMaster, i%512, TagJitterX, uint64(i/512))
	}
	_ = sink
}
