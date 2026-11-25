package hash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"goframe/internal/sim"
)

// 本文件证明三件事：
//   1. 混合前把每个 int32 截到低 16 位时，“低位相同、高位不同”的两个状态
//      喂给 FNV-1a 的输入完全一致，因而必然同指纹（构造条件见 truncatedState）。
//   2. 改成全 32 位无损混合后，同样的构造对输入不同，指纹必然不同，被判分叉。
//   3. 只加宽截断位宽（16 -> 32）并不治本：只要原生宽度仍大于截断宽度，
//      用同样手法在更宽的数上构造，冲突原样复现（见 TestWideningAloneFails）。

// mixTruncated 是故障版本：每个值先截到低 keep 位再做 FNV-1a。
// keep=16 即 uint16 截断；keep=32 演示“只加宽位宽”后的同类故障。
func mixTruncated(tick int, raw []int32, keep uint) uint64 {
	h := uint64(offset)
	mix := func(v uint64) {
		h ^= v
		h *= prime
	}
	mix(uint64(uint32(tick)) & ((uint64(1) << keep) - 1))
	for _, v := range raw {
		mix(uint64(uint32(v)) & ((uint64(1) << keep) - 1))
	}
	return h
}

// truncatedHash16 复现现网故障类：只混合每个字段的低 16 位。
func truncatedHash16(tick int, s *sim.State) uint64 {
	return mixTruncated(tick, s.Raw(), 16)
}

// construction 是一组“只改高位、低位保持一致”的分叉状态对。
type construction struct {
	name string
	why  string
	tick int
	grav int32
	rawA []int32
	rawB []int32
	// diffFields：A/B 在哪些字段上高位不同（这些字段低 16 位相同）。
	diffFields []int
	// classPerField：每个差异字段有多少种高位取值会被截成同一个低 16 位值。
	classPerField uint64
}

// 构造条件（三组共同满足）：
//   - 对每个差异字段 i，rawA[i] 与 rawB[i] 的低 16 位完全相等；
//   - 至少一个字段的高 16 位不同（保证 Raw() 不同，是两个真实分叉状态）；
//   - 其余字段与帧号完全一致，便于一眼看出冲突只来自高位。
func constructions() []construction {
	// 高 16 位全 1、低 16 位全 0 的 int32，即 0xFFFF0000（用左移避免常量溢出）。
	negHi := int32(-1 << 16)
	return []construction{
		{
			name: "c1-single-field",
			why:  "单实体，仅 velX 高位不同：0x00000001 vs 0xFFFF0001，低 16 位都是 0x0001",
			tick: 1, grav: 0,
			rawA:          []int32{0, 0, 0x00000001, 0},
			rawB:          []int32{0, 0, 0x00000001 | negHi, 0},
			diffFields:    []int{2},
			classPerField: 1 << 16,
		},
		{
			name: "c2-multi-field",
			why:  "双实体，velX 与 posY 两个字段高位不同；低 16 位分别保持 0x0001 / 0x0002",
			tick: 1, grav: 0,
			rawA: []int32{
				0, 0, 0x00000001, 0,
				0, 0x00000002, 0, 0,
			},
			rawB: []int32{
				0, 0, 0x00000001 | negHi, 0,
				0, 0x00000002 | negHi, 0, 0,
			},
			diffFields:    []int{2, 5},
			classPerField: 1 << 16,
		},
		{
			name: "c3-realistic",
			why:  "带重力的实际量：posX=1.0(0x00010000) 高位翻转，低位 0x0000 保持一致",
			tick: 7, grav: 2 << 16,
			rawA: []int32{
				0x00010000, 0, 0x00000003, 0,
			},
			rawB: []int32{
				0x00010000 | negHi, 0, 0x00000003, 0,
			},
			diffFields:    []int{0},
			classPerField: 1 << 16,
		},
	}
}

// archivedPair 是留档的状态对格式，写入 testdata/ 供人工对照。
type archivedPair struct {
	Construction string  `json:"construction"`
	Reason       string  `json:"reason"`
	Tick         int     `json:"tick"`
	GravityRaw   int32   `json:"gravity_raw"`
	StateA       []int32 `json:"state_a_raw"`
	StateB       []int32 `json:"state_b_raw"`
	Low16Equal   bool    `json:"low16_bits_equal"`
	RawDifferent bool    `json:"raw_different"`
}

func TestTruncatedHashCollidesAndFixedHashDiverges(t *testing.T) {
	cs := constructions()
	rows := make([]string, 0, len(cs))
	for _, c := range cs {
		a, err := sim.FromSlices(c.rawA, c.grav)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		b, err := sim.FromSlices(c.rawB, c.grav)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}

		// 前置条件：两个状态确实不同，且每个差异字段低 16 位一致。
		if equalRaw(a.Raw(), b.Raw()) {
			t.Fatalf("%s: 两个状态 Raw 相同，构造无效", c.name)
		}
		for _, i := range c.diffFields {
			if uint16(c.rawA[i]) != uint16(c.rawB[i]) {
				t.Fatalf("%s: 字段 %d 低 16 位不一致，不满足构造条件", c.name, i)
			}
			if c.rawA[i] == c.rawB[i] {
				t.Fatalf("%s: 字段 %d 高位未变化", c.name, i)
			}
		}

		// 证明一：截位混合必然同指纹。
		hA16 := truncatedHash16(c.tick, a)
		hB16 := truncatedHash16(c.tick, b)
		if hA16 != hB16 {
			t.Fatalf("%s: 截位哈希竟未碰撞，证明前提不成立", c.name)
		}

		// 证明二：全位混合后必然不同指纹（被判分叉）。
		fA := State(c.tick, a)
		fB := State(c.tick, b)
		if fA == fB {
			t.Fatalf("%s: 修复后仍碰撞，修复无效", c.name)
		}

		// 碰撞等价类大小：每个差异字段的高 16 位有 2^16 种取值，
		// 截断后都塌缩成同一个低 16 位值，字段间独立、连乘。
		classSize := uint64(1)
		for range c.diffFields {
			classSize *= c.classPerField
		}

		verdict := "分叉(不同指纹)"
		rows = append(rows, fmtRow(c.name, classSize, 16, hA16 == hB16, fA != fB, verdict))

		// 留档。
		rec := archivedPair{
			Construction: c.name,
			Reason:       c.why,
			Tick:         c.tick,
			GravityRaw:   c.grav,
			StateA:       c.rawA,
			StateB:       c.rawB,
			Low16Equal:   true,
			RawDifferent: !equalRaw(a.Raw(), b.Raw()),
		}
		writeArchive(t, c.name, rec)
	}

	t.Logf("\n三组构造对照（截断宽度=16，状态原生宽度=32）：\n%s", tableHeader()+joinRows(rows))
}

// TestWideningAloneFails 证明把截断宽度从 16 加到 32 不能根治：
// 在 64 位原生值上取两个“低 32 位相同、高 32 位不同”的数，uint32 截断后
// 混合输入再次相同，冲突按同一构造条件复现。
func TestWideningAloneFails(t *testing.T) {
	// 64 位构造对：低 32 位都是 0x00000001，高 32 位分别为 0 / 0x00000002。
	x := uint64(0x00000000_00000001)
	y := uint64(0x00000002_00000001)
	if uint32(x) != uint32(y) {
		t.Fatal("构造前提不成立：低 32 位应相同")
	}
	if x == y {
		t.Fatal("构造前提不成立：两个 64 位值应不同")
	}
	// 只加宽到 32 位的截断混合：两者输入都塌缩成 1，必然碰撞。
	hx := mixTruncated64(1, []uint64{x}, 32)
	hy := mixTruncated64(1, []uint64{y}, 32)
	if hx != hy {
		t.Fatal("uint32 截断竟未碰撞，加宽反例构造失败")
	}
	// 全 64 位无损混合则区分二者。
	if fnv64(1, []uint64{x}) == fnv64(1, []uint64{y}) {
		t.Fatal("全位混合不应碰撞")
	}
	t.Logf("\n只加宽位宽的反例：x=%016x y=%016x，uint32 截断后同为 %d，"+
		"截断哈希=%016x 相等；全 64 位混合后不同。", x, y, uint32(x), hx)
}

// ---- 64 位对照实现，用于“只加宽位宽”反例 ----

func mixTruncated64(tick uint64, vals []uint64, keep uint) uint64 {
	h := uint64(offset)
	mask := uint64(0)
	if keep == 64 {
		mask = ^uint64(0)
	} else {
		mask = (uint64(1) << keep) - 1
	}
	h ^= tick & mask
	h *= prime
	for _, v := range vals {
		h ^= v & mask
		h *= prime
	}
	return h
}

func fnv64(tick uint64, vals []uint64) uint64 {
	h := uint64(offset)
	h ^= tick
	h *= prime
	for _, v := range vals {
		h ^= v
		h *= prime
	}
	return h
}

// ---- 工具函数 ----

func equalRaw(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeArchive(t *testing.T, name string, rec archivedPair) {
	t.Helper()
	dir := filepath.Join("testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".json")
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func tableHeader() string {
	return "构造             冲突等价类大小  截断位宽  截位哈希判定  全位哈希判定\n" +
		"----------------- --------------- --------- ------------- ------------\n"
}

func fmtRow(name string, classSize uint64, keep uint, truncatedCollide, fixedDiffer bool, verdict string) string {
	tJudge := "一致(错误)"
	if !truncatedCollide {
		tJudge = "分叉"
	}
	fJudge := "一致"
	if fixedDiffer {
		fJudge = verdict
	}
	return pad(name, 17) + pad(itoa64(classSize), 16) + pad(itoa64(uint64(keep)), 10) +
		pad(tJudge, 13) + fJudge + "\n"
}

func joinRows(rows []string) string {
	out := ""
	for _, r := range rows {
		out += r
	}
	return out
}

// pad 按 rune 计数补空格，保证中文列对齐。
func pad(s string, n int) string {
	for len([]rune(s)) < n {
		s += " "
	}
	return s
}

func itoa64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
