package hash

import (
	"math/rand"
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

func TestHashOrderSensitive(t *testing.T) {
	a := sim.NewState(2, 0)
	b := sim.NewState(2, 0)
	a.Push(0, fixed.FromInt(1), 0)
	b.Push(1, fixed.FromInt(1), 0)
	if State(0, a) == State(0, b) {
		t.Fatal("顺序不同却同指纹")
	}
}

func TestHashStable(t *testing.T) {
	s := sim.NewState(3, fixed.FromInt(2))
	s.Integrate(1)
	if State(7, s) != State(7, s) {
		t.Fatal("同状态两次指纹不同")
	}
}

// TestIncrementalMatchesFullPerBit 是核心验收：随机指令与随机重力下，
// 逐帧比较增量与全量，要求每一位都相同，且 Check 三项差异恒为 0。
func TestIncrementalMatchesFullPerBit(t *testing.T) {
	for _, bodies := range []int{1, 2, 8, 64, 257} {
		for _, gravity := range []int{0, 1, -3, 977} {
			for _, sub := range []int{1, 4, 16} {
				for seed := int64(0); seed < 5; seed++ {
					runIncrementalVsFull(t, bodies, gravity, sub, seed)
				}
			}
		}
	}
}

func runIncrementalVsFull(t *testing.T, bodies, gravity, sub int, seed int64) {
	t.Helper()
	s := sim.NewState(bodies, fixed.FromInt(gravity))
	inc := NewIncremental(s)
	inc.SetTick(0)
	rng := rand.New(rand.NewSource(seed))

	for tick := 0; tick <= 40; tick++ {
		if tick > 0 {
			s.ClearDirty()
			// 随机选若干实体施加冲量，包含零冲量（不应标脏）。
			for k := 0; k < 1+rng.Intn(4); k++ {
				i := rng.Intn(bodies)
				dx := int32(rng.Intn(2001) - 1000)
				dy := int32(rng.Intn(2001) - 1000)
				s.Push(i, fixed.FromRaw(int(dx)), fixed.FromRaw(int(dy)))
			}
			for k := 0; k < sub; k++ {
				s.Integrate(sub)
			}
			inc.SetTick(tick)
			got := inc.Update()
			if got != inc.Value() {
				t.Fatalf("Update 返回值与 Value 不一致: %016x != %016x", got, inc.Value())
			}
		}
		eq := inc.Check(tick)
		if !eq.Equal() {
			t.Fatalf("bodies=%d gravity=%d sub=%d seed=%d tick=%d: 非逐位等价: %+v",
				bodies, gravity, sub, seed, tick, eq)
		}
		if eq.Full != State(tick, s) {
			t.Fatalf("全量两次计算不一致，指纹函数本身含状态")
		}
	}
}

// TestViolationDetectedCrossSlot 展示不满足等价条件时的后果：实体 j 被改动
// 却没有标脏（跨槽位影响漏报），增量与全量分叉，差异精确落在 j 这一项上。
//
// 这正是绕过 State 方法直接改内存的形态：j 的数值变了，脏集合里却没有 j。
func TestViolationDetectedCrossSlot(t *testing.T) {
	const bodies = 6
	s := sim.NewState(bodies, fixed.FromInt(3))
	for i := 0; i < bodies; i++ {
		s.Push(i, fixed.FromInt(i+1), fixed.FromInt(-i))
	}
	inc := NewIncremental(s)
	inc.SetTick(0)
	inc.Update()

	const j = 4
	oldRaw := s.RawAt(j)
	newRaw := oldRaw
	newRaw[0] += 12345 // 只改 j 的 posX
	s.DebugSetRaw(j, [4]fixed.Value{
		fixed.FromRaw(int(newRaw[0])), fixed.FromRaw(int(newRaw[1])),
		fixed.FromRaw(int(newRaw[2])), fixed.FromRaw(int(newRaw[3])),
	})
	// 故意不标脏 j，模拟“更新影响了别的槽位却没上报”。
	inc.SetTick(1)
	inc.Update()

	eq := inc.Check(1)
	if eq.Equal() {
		t.Fatal("跨槽位影响未被发现")
	}
	if eq.SlotDiffs != 1 || len(eq.BrokenSlots) != 1 || eq.BrokenSlots[0] != j {
		t.Fatalf("差异应精确落在槽位 %d，实际 %+v", j, eq)
	}
	if eq.HashDiff != 1 || eq.ProductDiff != 1 {
		t.Fatalf("终值与部分和都应不一致: %+v", eq)
	}
	if eq.Inc == eq.Full {
		t.Fatal("增量与全量撞上了同一个值，测试失去意义")
	}

	// 差异必须恰好等于 W_j * (D_j(S') - D_j(S))。
	n := bodies
	powP := primePowers(slotWidth*n + 1)
	want := slotWeight(j, n, powP) * (slotDigest(newRaw, j) - slotDigest(oldRaw, j))
	if got := eq.Full - eq.Inc; got != want {
		t.Fatalf("差异不落在 j 的单项上: got %016x want %016x", got, want)
	}
}

// TestAnySingleWordChangeDetected 验证 doc.go 的论断：单个 32 位字改动，
// 无论落在哪个槽位，都一定使指纹不同（不是概率性结论）。
func TestAnySingleWordChangeDetected(t *testing.T) {
	const bodies = 3
	base := sim.NewState(bodies, 0)
	for i := 0; i < bodies; i++ {
		base.Push(i, fixed.FromInt(i), fixed.FromInt(2*i))
	}
	baseTicks := []int{0, 1, -1, 1 << 30}
	for _, tick := range baseTicks {
		h0 := State(tick, base)
		for slot := 0; slot < bodies; slot++ {
			for word := 0; word < slotWidth; word++ {
				for _, delta := range []int32{1, -1, 1 << 15, -(1 << 30), 0x7fffffff} {
					s := sim.NewState(bodies, 0)
					raw := base.RawAt(slot)
					r := [4]fixed.Value{
						fixed.FromRaw(int(raw[0])), fixed.FromRaw(int(raw[1])),
						fixed.FromRaw(int(raw[2])), fixed.FromRaw(int(raw[3])),
					}
					cur := int32(r[word])
					r[word] = fixed.FromRaw(int(cur + delta))
					s.DebugSetRaw(slot, r)
					if State(tick, s) == h0 {
						t.Fatalf("tick=%d slot=%d word=%d delta=%d 改动未被检出",
							tick, slot, word, delta)
					}
				}
			}
		}
	}
}

// TestNoOpUpdatesDoNotDirty 验证数值没变时不会标脏，且多标脏也无害。
func TestNoOpUpdatesDoNotDirty(t *testing.T) {
	s := sim.NewState(4, 0)
	s.ClearDirty()
	s.Push(2, 0, 0)
	s.Integrate(1) // 全零状态，积分无变化
	if d := s.Dirty(); len(d) != 0 {
		t.Fatalf("无变化却标脏: %v", d)
	}

	// 同一槽位改两次只出现一次。
	s.Push(1, 1, 0)
	s.Push(1, 0, 1)
	if d := s.Dirty(); len(d) != 1 || d[0] != 1 {
		t.Fatalf("脏集合应去重且只含槽位 1: %v", d)
	}

	// 标脏但值回到原值：Check 仍应等价（增量加零）。
	inc := NewIncremental(s)
	inc.SetTick(0)
	inc.Update()
	s.Push(1, -1, -1) // 回到全零
	if eq := inc.Check(0); eq.SlotDiffs != 1 || eq.BrokenSlots[0] != 1 {
		t.Fatalf("未 Update 前应只看到槽位 1: %+v", eq)
	}
	inc.Update()
	if eq := inc.Check(0); !eq.Equal() {
		t.Fatalf("Update 后应等价: %+v", eq)
	}
}

// TestFullStateDigestSensitivity 用 32 位状态摘要作旁证：任何槽位改动都必须
// 反映在全量指纹里；这里比较的是不同状态之间的指纹差异。
func TestFullStateDigestSensitivity(t *testing.T) {
	var prev uint64
	for i := 0; i < 64; i++ {
		s := sim.NewState(5, 0)
		s.Push(i%5, fixed.FromRaw(i), 0)
		h := State(i%3, s)
		if i > 0 && h == prev {
			t.Fatalf("相邻状态指纹相同")
		}
		prev = h
	}
}

// TestViolationBreaksSequence 也是反例的一部分：一旦发生未标脏的跨槽位改动，
// 而该槽位此后不再被任何更新触及，后续帧的增量序列会一直与全量序列不同，
// 因为差值被永久吃进了部分和。若该槽位后来又被正常更新重算，差异才会自愈。
func TestViolationBreaksSequence(t *testing.T) {
	const bodies = 8
	const victim = 5 // 只被漏标的跨槽位写入触及，正常更新永远不碰它
	s := sim.NewState(bodies, 0)
	inc := NewIncremental(s)
	inc.SetTick(0)
	full := []uint64{State(0, s)}
	incs := []uint64{inc.Update()}

	for tick := 1; tick <= 5; tick++ {
		s.ClearDirty()
		if tick == 2 {
			raw := s.RawAt(victim)
			raw[1] += 999
			s.DebugSetRaw(victim, [4]fixed.Value{
				fixed.FromRaw(int(raw[0])), fixed.FromRaw(int(raw[1])),
				fixed.FromRaw(int(raw[2])), fixed.FromRaw(int(raw[3])),
			})
		}
		// 每帧只推实体 0..4，victim 的槽位不会被重算。
		s.Push(tick%5, fixed.FromInt(1), 0)
		inc.SetTick(tick)
		full = append(full, State(tick, s))
		incs = append(incs, inc.Update())
	}
	r := CompareSequences(full, incs)
	if r.DiffBits == 0 {
		t.Fatal("跨槽位漏标没有体现在序列差异上")
	}
	if r.FirstDiffAt != 2 {
		t.Fatalf("差异应从漏标的第 2 帧开始，实际 %d", r.FirstDiffAt)
	}
	if r.DiffFrames != 4 {
		t.Fatalf("漏标之后每帧都应不同，实际差异帧数 %d", r.DiffFrames)
	}

	// 反证自愈：让 victim 之后被正常更新一次，差异应当消失。
	s.ClearDirty()
	s.Push(victim, fixed.FromInt(1), 0)
	s.Push(victim, fixed.FromInt(-1), 0)
	inc.SetTick(6)
	inc.Update()
	if eq := inc.Check(6); !eq.Equal() {
		t.Fatalf("victim 被重算后应自愈: %+v", eq)
	}
}
