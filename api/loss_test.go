package api

import (
	"math/rand"
	"testing"
)

// TestDropLossEquivalence 验证「丢弃副本」造成的状态差，严格等于丢量向量：
//
//	delta_vel = L,  delta_pos = substeps * L（模 2^32）
//
// 其中 L 按 (实体, 分量) 求和为 Σ_g (k_g-1) d_g。多组丢量可以对消，
// 所以「某组非零」不等于「状态有差」；「状态无差」的充要条件是 L=0。
func TestDropLossEquivalence(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for iter := 0; iter < 2000; iter++ {
		n := rng.Intn(12)
		cmds := make([]Command, n)
		for i := range cmds {
			cmds[i] = Command{Body: rng.Intn(3), PushX: rng.Intn(5) - 2, PushY: rng.Intn(5) - 2}
		}
		sub := 1 + rng.Intn(4)
		opt := Options{Bodies: 3, SubSteps: sub}

		wOrig, _ := New(opt)
		if err := wOrig.Step(cmds); err != nil {
			t.Fatal(err)
		}
		wDrop, _ := New(opt)
		if err := wDrop.Step(dropDuplicates(cmds)); err != nil {
			t.Fatal(err)
		}

		// 期望丢量：按实体与分量求和。
		lx := make([]int32, 3)
		ly := make([]int32, 3)
		for _, l := range DropLoss(cmds) {
			lx[l.Body] += int32(l.LostX)
			ly[l.Body] += int32(l.LostY)
		}

		so, sd := wOrig.Snapshot(), wDrop.Snapshot()
		for b := 0; b < 3; b++ {
			wantVelX := lx[b]
			wantVelY := ly[b]
			wantPosX := int32(int64(sub) * int64(lx[b]))
			wantPosY := int32(int64(sub) * int64(ly[b]))
			// 原序 - 丢弃 = 丢量；丢弃的速度/位置比原序少这么多。
			if got := int32(so[4*b+2]) - int32(sd[4*b+2]); got != wantVelX {
				t.Fatalf("iter=%d body=%d velX 差 %d 期望 %d，输入 %+v", iter, b, got, wantVelX, cmds)
			}
			if got := int32(so[4*b+3]) - int32(sd[4*b+3]); got != wantVelY {
				t.Fatalf("iter=%d body=%d velY 差 %d 期望 %d", iter, b, got, wantVelY)
			}
			if got := int32(so[4*b+0]) - int32(sd[4*b+0]); got != wantPosX {
				t.Fatalf("iter=%d body=%d posX 差 %d 期望 %d（sub=%d）", iter, b, got, wantPosX, sub)
			}
			if got := int32(so[4*b+1]) - int32(sd[4*b+1]); got != wantPosY {
				t.Fatalf("iter=%d body=%d posY 差 %d 期望 %d（sub=%d）", iter, b, got, wantPosY, sub)
			}
		}

		// 归并永远与丢量无关，逐项等于原序。
		wMerge, _ := New(opt)
		if err := wMerge.Step(DedupCommands(cmds)); err != nil {
			t.Fatal(err)
		}
		for i := range so {
			if so[i] != wMerge.Snapshot()[i] {
				t.Fatalf("iter=%d 归并第 %d 项 %d vs %d，输入 %+v", iter, i, wMerge.Snapshot()[i], so[i], cmds)
			}
		}
		// 状态相等 != 每组都非零（多组丢量可按分量对消），但状态相等 iff
		// 每个 (实体, 分量) 的总丢量 L_b 都为 0。
		allZero := true
		for b := 0; b < 3; b++ {
			if lx[b] != 0 || ly[b] != 0 {
				allZero = false
			}
		}
		eq := wOrig.Hash() == wDrop.Hash()
		if eq != allZero {
			t.Fatalf("iter=%d 状态相等=%v 但分量总丢量全零=%v，输入 %+v", iter, eq, allZero, cmds)
		}
	}
}

// TestDropLossModularException 钉住模 2³² 的反常点：m=3、d=2³¹ 时
// (m-1)d = 2³² ≡ 0，交集非空但丢弃无损；m=2 时同一 d 仍有损。
func TestDropLossModularException(t *testing.T) {
	for _, tc := range []struct {
		m    int
		want bool // 期望「丢弃后与原序相同」
	}{
		{m: 2, want: false},
		{m: 3, want: true},  // (m-1)=2 偶
		{m: 4, want: false}, // (m-1)=3 奇
		{m: 5, want: true},
		{m: 7, want: true}, // (m-1)=6 偶
		{m: 9, want: true},
	} {
		cmds := make([]Command, tc.m)
		for i := range cmds {
			cmds[i] = Command{Body: 0, PushX: -1 << 31}
		}
		opt := Options{Bodies: 1, SubSteps: 1}
		orig, err := New(opt)
		if err != nil {
			t.Fatal(err)
		}
		_ = orig.Step(cmds)
		dropped, err := New(opt)
		if err != nil {
			t.Fatal(err)
		}
		_ = dropped.Step(dropDuplicates(cmds))
		if got := orig.Hash() == dropped.Hash(); got != tc.want {
			t.Fatalf("m=%d 丢弃无损=%v 期望 %v (d=-2^31)", tc.m, got, tc.want)
		}
	}
}
