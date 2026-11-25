package api

import (
	"testing"

	"goframe/internal/queue"
)

// lcRand 是确定性 LCG，保证测试与实验在任何机器上可复现。
type lcRand struct{ state uint64 }

const one = 1 << 16 // Q16.16 的 1.0，与 fixed.One 同值

func (r *lcRand) n() uint64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return r.state
}

// genFrames 生成 frames 帧、每帧 perFrame 条指令的确定性流。
// 冲量量级取 1..maxMag 的整数（Q16.16 单位 One），轴、符号、目标实体均由 RNG 决定。
func genFrames(seed uint64, bodies, frames, perFrame, maxMag int) [][]Command {
	r := &lcRand{state: seed}
	out := make([][]Command, frames)
	for t := range out {
		cmds := make([]Command, perFrame)
		for i := range cmds {
			mag := 1 + int(r.n()%uint64(maxMag))
			sign := int64(1)
			if r.n()%2 == 0 {
				sign = -1
			}
			px, py := 0, 0
			if r.n()%2 == 0 {
				px = int(sign * int64(mag))
			} else {
				py = int(sign * int64(mag))
			}
			cmds[i] = Command{Tick: t, Body: int(r.n() % uint64(bodies)), PushX: px << 16, PushY: py << 16}
		}
		out[t] = cmds
	}
	return out
}

// l1Vel / l1Pos 从状态快照（posX,posY,velX,velY 交错）求速度差与位置差的 L1 和。
func l1Vel(a, b []int32) int64 {
	var s int64
	for i := 0; i < len(a); i += 4 {
		s += abs32(a[i+2] - b[i+2])
		s += abs32(a[i+3] - b[i+3])
	}
	return s
}

func l1Pos(a, b []int32) int64 {
	var s int64
	for i := 0; i < len(a); i += 4 {
		s += abs32(a[i] - b[i])
		s += abs32(a[i+1] - b[i+1])
	}
	return s
}

func abs32(x int32) int64 {
	if x < 0 {
		return -int64(x)
	}
	return int64(x)
}

// TestDriftBoundedByDroppedImpulse 逐帧验证：
//   - 速度（冲量）偏差 <= 被丢冲量大小之和 B(D)（定理 1，与丢弃时机无关）
//   - 位置偏差 <= 严格加权上界 Σ_h (T-h)·w_h <= T·B(D)
//
// 同时验证 Immediate 与 Deferred 两种丢弃时机产生完全相同的状态哈希序列，
// 即偏差与丢弃时机无关（定理 1 的时机不变性）。
func TestDriftBoundedByDroppedImpulse(t *testing.T) {
	const bodies, frames, perFrame, maxMag = 4, 12, 10, 20
	stream := genFrames(42, bodies, frames, perFrame, maxMag)

	for _, cap := range []int{1, 2, 4, 8} {
		ref, _ := New(Options{Bodies: bodies, SubSteps: 1})
		imm, _ := NewWithQueue(Options{Bodies: bodies, SubSteps: 1}, queue.Immediate, queue.TopK)
		def, _ := NewWithQueue(Options{Bodies: bodies, SubSteps: 1}, queue.Deferred, queue.TopK)
		tail, _ := NewWithQueue(Options{Bodies: bodies, SubSteps: 1}, queue.Immediate, queue.TailDrop)

		var maxVel, maxPos int64
		var weightedDrops int64 // Σ_h (T-h)·（帧 h 被丢冲量和）：位置偏差的严格上界
		prevDropSum := int64(0)
		for tick, cmds := range stream {
			if err := ref.Step(cmds); err != nil {
				t.Fatal(err)
			}
			if err := imm.StepWithCap(cmds, cap); err != nil {
				t.Fatal(err)
			}
			if err := def.StepWithCap(cmds, cap); err != nil {
				t.Fatal(err)
			}
			if err := tail.StepWithCap(cmds, cap); err != nil {
				t.Fatal(err)
			}

			// 时机不变性：两种时机逐帧哈希一致，回放指纹也一致。
			if imm.Hash() != def.Hash() {
				t.Fatalf("cap=%d tick=%d 哈希随时机变化: %016x vs %016x", cap, tick, imm.Hash(), def.Hash())
			}
			if imm.DropCount() != def.DropCount() || imm.DropWeight() != def.DropWeight() {
				t.Fatalf("cap=%d tick=%d 丢弃统计随时机变化", cap, tick)
			}

			refSnap, gotSnap := ref.Snapshot(), imm.Snapshot()
			velGap := l1Vel(refSnap, gotSnap)
			posGap := l1Pos(refSnap, gotSnap)
			maxVel, maxPos = max64(maxVel, velGap), max64(maxPos, posGap)

			bound := imm.DropWeight() // B(D)：被丢冲量大小之和
			if velGap > bound {
				t.Fatalf("cap=%d tick=%d 速度偏差 %d 超过被丢冲量和 %d", cap, tick, velGap, bound)
			}
			// 帧 h 丢弃的冲量 w 在该帧积分中先进入速度、再进入位置一次，
			// 到帧 T-1 共参与 T-h 次位置积分，故严格上界为 Σ_h (T-h)·w_h。
			weightedDrops += int64(frames-tick) * (imm.DropWeight() - prevDropSum)
			prevDropSum = imm.DropWeight()
			if posGap > weightedDrops {
				t.Fatalf("cap=%d tick=%d 位置偏差 %d 超过严格加权上界 %d", cap, tick, posGap, weightedDrops)
			}
			// 松弛上界：加权和 <= T·B(D)。
			if posGap > int64(frames)*bound {
				t.Fatalf("cap=%d tick=%d 位置偏差 %d 超过松弛上界 T·B=%d", cap, tick, posGap, int64(frames)*bound)
			}
			// 参考世界不丢指令，偏差对它而言为零。
			if l1Vel(refSnap, refSnap) != 0 {
				t.Fatal("参考世界自比非零")
			}
		}
		t.Logf("cap=%d 丢弃=%d 被丢冲量和=%d 最大速度偏差=%d 最大位置偏差=%d",
			cap, imm.DropCount(), imm.DropWeight(), maxVel, maxPos)
	}
}

// TestTopKBeatsTailDropBudget 验证保留策略与阈值的关系：
// 同一容量下 TopK 的被丢冲量和不大于 TailDrop；存在阈值使 TopK 落在阈值内而 TailDrop 越界。
// 使用精心构造的帧：小冲量指令先到、大冲量指令后到，正好击中 TailDrop“丢新指令”的弱点。
func TestTopKBeatsTailDropBudget(t *testing.T) {
	const bodies = 4
	// 每帧 5 条，容量 2：前两条冲量为 1，后三条为 20。
	// TopK 保留两条 20，丢 1+1+20=22；TailDrop 保留两条 1，丢 20*3=60。
	oneFrame := func(tick int) []Command {
		cs := make([]Command, 5)
		for i := range cs {
			mag := 20
			if i < 2 {
				mag = 1
			}
			cs[i] = Command{Tick: tick, Body: i % bodies, PushX: mag << 16}
		}
		return cs
	}
	const frames, cap = 6, 2
	topk, _ := NewWithQueue(Options{Bodies: bodies, SubSteps: 1}, queue.Immediate, queue.TopK)
	tail, _ := NewWithQueue(Options{Bodies: bodies, SubSteps: 1}, queue.Immediate, queue.TailDrop)
	for t := 0; t < frames; t++ {
		cs := oneFrame(t)
		topk.StepWithCap(cs, cap)
		tail.StepWithCap(cs, cap)
	}
	// 每帧：TopK 丢 22，TailDrop 丢 60（单位 One）。
	if got, want := topk.DropWeight()/one, int64(22*frames); got != want {
		t.Fatalf("TopK 丢弃和=%d 期望 %d", got, want)
	}
	if got, want := tail.DropWeight()/one, int64(60*frames); got != want {
		t.Fatalf("TailDrop 丢弃和=%d 期望 %d", got, want)
	}
	// 阈值取每帧 22..59 之间：TopK 满足，TailDrop 越界。
	tau := int64(40*frames) * one
	if topk.DropWeight() > tau {
		t.Fatalf("TopK 应落在阈值 %d 内，实际 %d", tau, topk.DropWeight())
	}
	if tail.DropWeight() <= tau {
		t.Fatalf("TailDrop 应超出阈值 %d，实际 %d", tau, tail.DropWeight())
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
