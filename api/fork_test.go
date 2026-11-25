package api

import (
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

// TestForkedHighBitsDetected 在对外入口验证修复：
// 两个状态仅某字段高 16 位不同、低 16 位一致，对拍必须判成分叉，
// 不能再像截位指纹那样误判为一致。
func TestForkedHighBitsDetected(t *testing.T) {
	// 两个世界跑完全相同的指令，先确认正常情况下指纹一致（基线无误判）。
	opt := Options{Bodies: 1, SubSteps: 1, Gravity: 0}
	cmds := []Command{{Tick: 0, Body: 0, PushX: 1 << 16}}

	wa, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	wb, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := wa.Step(cmds); err != nil {
		t.Fatal(err)
	}
	if err := wb.Step(cmds); err != nil {
		t.Fatal(err)
	}
	if wa.Hash() != wb.Hash() {
		t.Fatal("同指令序列的两个世界指纹应一致")
	}

	// 直接构造一对仅高位不同的状态，替换其中一个世界的内部状态不可行（字段私有），
	// 因此这里用 sim 层构造并经 api 暴露的 Snapshot 对比 + hash.State 对拍。
	negHi := int32(-1 << 16)
	sa, err := sim.FromSlices([]int32{0, 0, 0x00000001, 0}, 0)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := sim.FromSlices([]int32{0, 0, 0x00000001 | negHi, 0}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 低位一致、整体不同，是真实的分叉状态对。
	if sa.Raw()[2] == sb.Raw()[2] {
		t.Fatal("构造无效：两字段应不同")
	}
	if fixed.Value(sa.Raw()[2]).Raw()&0xFFFF != fixed.Value(sb.Raw()[2]).Raw()&0xFFFF {
		t.Fatal("构造无效：低 16 位应一致")
	}

	// 通过两个 api.World 各跑一步后，用 Snapshot 不可注入；
	// 这里直接对拍 sim 状态的指纹（api.Hash 内部即 hash.State）。
	ha := fingerprint(1, sa)
	hb := fingerprint(1, sb)
	if ha == hb {
		t.Fatalf("高位分叉未被检出，两状态同指纹 %016x", ha)
	}
	t.Logf("分叉已检出：%016x != %016x", ha, hb)
}
