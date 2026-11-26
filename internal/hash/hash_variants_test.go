package hash

import (
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

// 构造两个仅实体 0 的 posX 相差 delta 的状态。
func diffStates(t *testing.T, delta int32) (a, b *sim.State) {
	t.Helper()
	a = sim.NewState(2, 0)
	b = sim.NewState(2, 0)
	// 用 Push + Integrate 把任意 delta 落进 posX：
	// Push(delta) 设速度，Integrate 后 posX 增加 delta，速度仍为 delta；
	// 再 Push(-delta) 把速度清零，最终两状态只有 posX 不同。
	b.Push(0, fixed.FromRaw(int(delta)), 0)
	b.Integrate(1)
	b.Push(0, fixed.FromRaw(int(-delta)), 0)
	return a, b
}

func TestStateFullWidthCatchesSubUnit(t *testing.T) {
	a, b := diffStates(t, 1) // 1 raw = 1/65536 世界单位
	if State(4, a) == State(4, b) {
		t.Fatal("全 32 位哈希不应漏掉 1 raw 的位置差")
	}
}

func TestStateDropLowBucketCollision(t *testing.T) {
	// 丢低 8 位：|Δ|<256 且两端落在同一桶时必须碰撞；
	// 选 Δ=1 且基准对齐 r=0（0 与 1 同桶）。
	a, b := diffStates(t, 1)
	if StateDropLow(4, a, 8) != StateDropLow(4, b, 8) {
		t.Fatal("丢低 8 位时同桶的 1 raw 差异应碰撞")
	}
	// Δ=256 恰好跨一个桶边界，必须可区分。
	c, d := diffStates(t, 256)
	if StateDropLow(4, c, 8) == StateDropLow(4, d, 8) {
		t.Fatal("丢低 8 位时跨桶的 256 raw 差异不应碰撞")
	}
}

func TestStateKeepLowDivisibility(t *testing.T) {
	// 只留低 16 位：Δ=3*65536 是 2^16 的整数倍，必漏。
	a, b := diffStates(t, 3*65536)
	if StateKeepLow(4, a, 16) != StateKeepLow(4, b, 16) {
		t.Fatal("只取低 16 位时 3 个整数单位的差异应被漏掉")
	}
	// Δ=65537 不能被 2^16 整除，必须可区分。
	c, d := diffStates(t, 65537)
	if StateKeepLow(4, c, 16) == StateKeepLow(4, d, 16) {
		t.Fatal("只取低 16 位时非整除差异不应碰撞")
	}
}

func TestZeroTruncationEqualsFull(t *testing.T) {
	s := sim.NewState(3, fixed.FromInt(2))
	s.Integrate(1)
	if StateDropLow(7, s, 0) != State(7, s) {
		t.Fatal("dropBits=0 必须与全位哈希一致")
	}
	if StateKeepLow(7, s, 32) != State(7, s) {
		t.Fatal("keepBits=32 必须与全位哈希一致")
	}
}
