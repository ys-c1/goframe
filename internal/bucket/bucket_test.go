package bucket

import "testing"

// TestDivModUnique 用穷举扫描验证欧几里得商余数对的唯一性，
// 并验证 Floor(t,F) 恰好是唯一满足 qF <= t < (q+1)F 的整数 q。
func TestDivModUnique(t *testing.T) {
	for F := int64(1); F <= 64; F++ {
		for tt := int64(-300); tt <= 300; tt++ {
			q, r := DivMod(tt, F)
			if r < 0 || r >= F {
				t.Fatalf("F=%d t=%d 余数越界 r=%d", F, tt, r)
			}
			if got := q*F + r; got != tt {
				t.Fatalf("分解不成立: %d*%d+%d=%d != %d", q, F, r, got, tt)
			}
			// 唯一性：在候选 q 附近只有一个满足半开区间条件。
			var hits []int64
			for cand := q - 2; cand <= q+2; cand++ {
				if cand*F <= tt && tt < (cand+1)*F {
					hits = append(hits, cand)
				}
			}
			if len(hits) != 1 || hits[0] != q {
				t.Fatalf("F=%d t=%d 半开区间命中 %v，应只有 %d", F, tt, hits, q)
			}
			if FloorIndex(tt, F) != q {
				t.Fatalf("Floor=%d != DivMod 商 %d", FloorIndex(tt, F), q)
			}
		}
	}
}

// TestHalfOpenIsNecessary 说明闭区间会在边界点产生两个归属：
// t=(q+1)F 同时属于 [qF,(q+1)F] 与 [(q+1)F,(q+2)F]。
func TestClosedIntervalDoubleOwnership(t *testing.T) {
	F := int64(8)
	for q := int64(0); q < 5; q++ {
		boundary := (q + 1) * F
		inLeft := q*F <= boundary && boundary <= (q+1)*F
		inRight := (q+1)*F <= boundary && boundary <= (q+2)*F
		if !(inLeft && inRight) {
			t.Fatal("闭区间边界应同时属于左右两桶")
		}
		// 半开区间 [qF,(q+1)F) 下边界只属于右桶，归属唯一。
		leftHalfOpen := q*F <= boundary && boundary < (q+1)*F
		rightHalfOpen := (q+1)*F <= boundary && boundary < (q+2)*F
		if leftHalfOpen || !rightHalfOpen {
			t.Fatal("半开区间边界应只属于右桶")
		}
	}
}

// TestRoundTieSameTimestampTwoFrames 见证“同一时间戳落相邻两帧”：
// 偶数 F 时 c_k=(2k+1)F/2 是整数精确平局点，HalfUp 与 HalfDown
// 对同一个 t 给出 k+1 与 k。
func TestRoundTieSameTimestampTwoFrames(t *testing.T) {
	for _, F := range []int64{8, 16, 32} {
		for k := int64(0); k < 4; k++ {
			if !IsIntegralTie(k, F) {
				t.Fatalf("偶数 F=%d 下 c_%d 应为整数平局点", F, k)
			}
			tstar := RoundBoundary2(k, F) / 2
			up := Frame(tstar, F, RoundHalfUp)
			down := Frame(tstar, F, RoundHalfDown)
			if up != k+1 || down != k {
				t.Fatalf("F=%d t*=%d: up=%d down=%d，应为 %d/%d",
					F, tstar, up, down, k+1, k)
			}
			if up-down != 1 {
				t.Fatalf("两版应只差一帧，实际 %d", up-down)
			}
		}
	}
	// 奇数 F 时 c_k 是半整数，整数时间戳没有精确平局点。
	if IsIntegralTie(1, 5) {
		t.Fatal("F=5 不应存在整数精确平局点")
	}
}

// TestAmbiguityBandWidth 验证模糊带判定：带宽 2e，边界两侧 e 内可翻转。
func TestAmbiguityBandWidth(t *testing.T) {
	F, e, k := int64(16), int64(1), int64(2)
	c2 := RoundBoundary2(k, F) // 2*c_k = 80，c_k=40
	if c2 != 80 {
		t.Fatalf("2*c_2=%d, want 80", c2)
	}
	// t=39 与 t=40 都在 e=1 带内；39 四舍五入到 2，40 平局向上到 3。
	if !InAmbiguityBand(39, k, F, e) || !InAmbiguityBand(40, k, F, e) {
		t.Fatal("39/40 应在模糊带内")
	}
	if InAmbiguityBand(38, k, F, e) {
		t.Fatal("38 距边界 2，超出 e=1 带")
	}
	if Frame(39, F, RoundHalfUp) != 2 || Frame(40, F, RoundHalfUp) != 3 {
		t.Fatal("39 应入帧2，40 平局应入帧3")
	}
}

// TestFloorPermutationInvariant 量化版唯一性：同一批时间戳无论什么顺序，
// Floor 分桶结果（按帧计数）必须一致。
func TestFloorPermutationInvariant(t *testing.T) {
	ts := []int64{12, 40, 112, 13, 41, 111, 7, 24}
	for _, F := range []int64{8, 16, 32} {
		base := map[int64]int{}
		for _, x := range ts {
			base[FloorIndex(x, F)]++
		}
		for iter := 0; iter < 5; iter++ {
			got := map[int64]int{}
			perm := []int64{ts[7], ts[0], ts[3], ts[5], ts[1], ts[6], ts[2], ts[4]}
			for _, x := range perm {
				got[Frame(x, F, Floor)]++
			}
			if len(got) != len(base) {
				t.Fatalf("F=%d 排列后桶集合变化", F)
			}
			for k, v := range base {
				if got[k] != v {
					t.Fatalf("F=%d 桶 %d 计数 %d != %d", F, k, got[k], v)
				}
			}
		}
	}
}
