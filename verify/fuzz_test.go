package verify

import (
	"math/rand"
	"testing"

	"goframe/api"
	"goframe/replay"
)

// TestSubStepInvariance_SeededStreams 在 40 个固定种子的“脏”指令流上
// （随机实体数、含负/零重力、正负且不对齐的原始冲量、随机帧数），
// 逐帧比较 1/2/4/8/16 档哈希，任何一帧不一致即失败。
func TestSubStepInvariance_SeededStreams(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewSource(seed * 7919))
		bodies := 1 + rng.Intn(12)
		gravity := rng.Intn(21) - 10
		framesN := 50 + rng.Intn(150)

		frames := make([]replay.Frame, framesN)
		for f := range frames {
			nc := rng.Intn(5)
			cmds := make([]api.Command, 0, nc)
			for k := 0; k < nc; k++ {
				cmds = append(cmds, api.Command{
					Tick:  f,
					Body:  rng.Intn(bodies),
					PushX: rng.Intn(4_000_001) - 2_000_000,
					PushY: rng.Intn(4_000_001) - 2_000_000,
				})
			}
			frames[f] = replay.Frame{Commands: cmds}
		}

		ref, err := Run(bodies, 1, gravity, frames)
		if err != nil {
			t.Fatalf("seed=%d: %v", seed, err)
		}
		for _, sub := range []int{2, 4, 8, 16} {
			cur, err := Run(bodies, sub, gravity, frames)
			if err != nil {
				t.Fatalf("seed=%d sub=%d: %v", seed, sub, err)
			}
			for f := 0; f < framesN; f++ {
				if cur.Hashes[f] != ref.Hashes[f] {
					t.Fatalf("seed=%d frame=%d sub=%d hash=%016x != sub1=%016x",
						seed, f, sub, cur.Hashes[f], ref.Hashes[f])
				}
			}
		}
	}
}
