package alloc

import "testing"

// TestRecycledHashDiverges 证明槽位复用会打乱状态槽位：
// 同终态物理数值的三组增删序列，复用引擎指纹两两不同；
// 且每个序列相对零复用基线的哈希流错位字数 = 复用次数 × 槽位字数。
func TestRecycledHashDiverges(t *testing.T) {
	for _, rep := range RunAll() {
		base := rep.Sequences[2] // 零复用序列
		for i := 0; i < 3; i++ {
			sr := rep.Sequences[i]
			// 恒等式一：高水位 = 总生成 - 复用，与分配器实现无关。
			if got := sr.TotalSpawns - sr.Reuses; got != sr.HighWater {
				t.Fatalf("%s seq%d: 高水位恒等式不成立 %d != %d",
					rep.Scenario, i, got, sr.HighWater)
			}
			// 恒等式二：错位字数 = 总槽位差 × 每槽位字数 = 复用次数 × 4。
			shift := len(base.RecycledRaw) - len(sr.RecycledRaw)
			if shift != sr.Reuses*slotWords {
				t.Fatalf("%s seq%d: 错位量 %d != 复用次数×4=%d",
					rep.Scenario, i, shift, sr.Reuses*slotWords)
			}
		}
		// 三个复用引擎哈希必须两两不同（同物理状态、不同槽位排列）。
		h0, h1, h2 := rep.Sequences[0].RecycledHash, rep.Sequences[1].RecycledHash, rep.Sequences[2].RecycledHash
		if h0 == h1 || h1 == h2 || h0 == h2 {
			t.Fatalf("%s: 槽位复用引擎未产生哈希分叉 %016x %016x %016x",
				rep.Scenario, h0, h1, h2)
		}
	}
}

// TestMonotonicHashOrderIndependent 证明单调递增、永不复用的编号
// 让指纹与增删顺序无关：三组序列终态指纹完全一致，且哈希输入流相同。
func TestMonotonicHashOrderIndependent(t *testing.T) {
	for _, rep := range RunAll() {
		h0, h1, h2 := rep.Sequences[0].MonoHash, rep.Sequences[1].MonoHash, rep.Sequences[2].MonoHash
		if h0 != h1 || h1 != h2 {
			t.Fatalf("%s: 单调引擎哈希与增删顺序有关 %016x %016x %016x",
				rep.Scenario, h0, h1, h2)
		}
		r0, r1, r2 := rep.Sequences[0].MonoRaw, rep.Sequences[1].MonoRaw, rep.Sequences[2].MonoRaw
		if len(r0) != len(r1) || len(r1) != len(r2) {
			t.Fatalf("%s: 单调引擎哈希流长度不一致", rep.Scenario)
		}
		for i := range r0 {
			if r0[i] != r1[i] || r1[i] != r2[i] {
				t.Fatalf("%s: 单调引擎哈希流第 %d 字不一致", rep.Scenario, i)
			}
		}
	}
}
