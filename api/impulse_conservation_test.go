package api

import (
	"testing"

	"goframe/internal/impulse"
)

// 三档子步数（都不整除 1<<16，修复前每帧都有非零余数损失）。
var conservationSubSteps = []int{3, 5, 7}

const (
	conservationGravity = 2
	conservationFrames  = 1000
)

// velYIndex 返回 Snapshot 中第 body 个实体 velY 的下标。
func velYIndex(body int) int { return 4*body + 3 }

// TestPerFrameGravityImpulseConserved 是题目要求的硬性验收：
// 三档子步数下，逐帧重力冲量都必须等于设定值，
// 且三档之间的逐帧差值恒为 0（任意一帧非零即失败）。
func TestPerFrameGravityImpulseConserved(t *testing.T) {
	setpoint := int64(conservationGravity) << 16

	// 先证明这组参数在旧的整除做法下确实丢冲量，避免测试“没有牙”。
	for _, s := range conservationSubSteps {
		if _, _, loss := impulse.Naive(setpoint, int64(s)); loss == 0 {
			t.Fatalf("参数选取无效：sub=%d 时整除余数为 0，无法检验补偿", s)
		}
	}

	// impulses[s][f]：第 s 档第 f 帧每实体实际重力冲量。
	impulses := make([][]int64, len(conservationSubSteps))
	for si, sub := range conservationSubSteps {
		w, err := New(Options{Bodies: 4, SubSteps: sub, Gravity: conservationGravity})
		if err != nil {
			t.Fatal(err)
		}
		col := make([]int64, conservationFrames)
		for f := 0; f < conservationFrames; f++ {
			before := w.Snapshot()
			if err := w.Step(nil); err != nil {
				t.Fatal(err)
			}
			after := w.Snapshot()

			// 口径一：内核精确台账。
			ledger := w.FrameGravityImpulse()
			if ledger != setpoint {
				t.Fatalf("sub=%d frame=%d 台账冲量=%d 设定值=%d 差=%d",
					sub, f, ledger, setpoint, ledger-setpoint)
			}

			// 口径二：公开 Snapshot 里 velY 的逐帧模差（对 int32 回绕稳健）。
			for body := 0; body < w.opt.Bodies; body++ {
				idx := velYIndex(body)
				delta := int64(after[idx] - before[idx]) // int32 模减后符号扩展
				if delta != setpoint {
					t.Fatalf("sub=%d frame=%d body=%d velY增量=%d 设定值=%d 差=%d",
						sub, f, body, delta, setpoint, delta-setpoint)
				}
			}
			col[f] = ledger
		}
		// 累计值也必须恰好等于 F*G。
		if got := w.TotalGravityImpulse(); got != int64(conservationFrames)*setpoint {
			t.Fatalf("sub=%d 累计冲量=%d 期望=%d", sub, got, int64(conservationFrames)*setpoint)
		}
		impulses[si] = col
	}

	// 三档子步数的逐帧冲量互相比对：差值必须恒为 0。
	for f := 0; f < conservationFrames; f++ {
		for a := 0; a < len(conservationSubSteps); a++ {
			for b := a + 1; b < len(conservationSubSteps); b++ {
				d := impulses[a][f] - impulses[b][f]
				if d != 0 {
					t.Fatalf("frame=%d sub=%d vs %d 冲量差=%d（必须恒为 0）",
						f, conservationSubSteps[a], conservationSubSteps[b], d)
				}
			}
		}
	}
}

// TestAllAllowedSubStepsConserved 对 Options 允许的全部子步档 1..16 做逐帧守恒，
// 这样无论“三档”取 1/2/4、1/4/8 还是非 2 的幂档，结论都被直接覆盖。
func TestAllAllowedSubStepsConserved(t *testing.T) {
	const frames = 200
	setpoint := int64(conservationGravity) << 16
	for S := 1; S <= 16; S++ {
		w, err := New(Options{Bodies: 2, SubSteps: S, Gravity: conservationGravity})
		if err != nil {
			t.Fatal(err)
		}
		for f := 0; f < frames; f++ {
			before := w.Snapshot()
			if err := w.Step(nil); err != nil {
				t.Fatal(err)
			}
			after := w.Snapshot()
			if got := w.FrameGravityImpulse(); got != setpoint {
				t.Fatalf("S=%d frame=%d ledger=%d want=%d", S, f, got, setpoint)
			}
			for body := 0; body < 2; body++ {
				idx := velYIndex(body)
				if d := int64(after[idx] - before[idx]); d != setpoint {
					t.Fatalf("S=%d frame=%d body=%d velY delta=%d want=%d", S, f, body, d, setpoint)
				}
			}
		}
	}
}

// TestCarryIdentityThroughWorld 保证跨帧余数台账的逐帧复式恒等式
// 在与内核相同的参数下连续 1000 帧差值为 0，且误差不越界。
func TestCarryIdentityThroughWorld(t *testing.T) {
	G := int64(conservationGravity) << 16
	for _, sub := range conservationSubSteps {
		c := &impulse.FrameCarry{}
		var sum int64
		for f := 0; f < conservationFrames; f++ {
			_, delivered, remIn, remOut := c.CarryFrame(G, int64(sub))
			if d := impulse.IdentityDiff(G, delivered, remIn, remOut); d != 0 {
				t.Fatalf("sub=%d frame=%d carry identity diff=%d", sub, f, d)
			}
			sum += delivered
		}
		if sum != int64(conservationFrames)*G-c.Rem {
			t.Fatalf("sub=%d 望远镜守恒失败", sub)
		}
	}
}
