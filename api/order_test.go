package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 实验参数：4 实体、4 子步、重力 2，冲量取带低位零头的原始值，
// 使任何隐藏的截断/非线性都无处遁形。
var orderOpt = Options{Bodies: 4, SubSteps: 4, Gravity: 2}

var (
	cmdA1 = Command{Tick: 0, Body: 0, PushX: 100003, PushY: -65537}
	cmdA2 = Command{Tick: 0, Body: 0, PushX: -77777, PushY: 131073}
	cmdB1 = Command{Tick: 0, Body: 1, PushX: 32771, PushY: -131072}
	cmdB2 = Command{Tick: 0, Body: 1, PushX: 65539, PushY: 9999}
)

// runOrder 用给定首帧指令跑 totalFrames 帧（后续帧为空），返回末态哈希与快照。
func runOrder(t *testing.T, first []Command, totalFrames int) (uint64, []int32) {
	t.Helper()
	w, err := New(orderOpt)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < totalFrames; i++ {
		var cmds []Command
		if i == 0 {
			cmds = first
		}
		if err := w.Step(cmds); err != nil {
			t.Fatal(err)
		}
	}
	return w.Hash(), w.Snapshot()
}

// diffSlots 逐项比对两个快照，返回非零项数与每项差值。
func diffSlots(a, b []int32) (nonZero int, diffs []int64) {
	diffs = make([]int64, len(a))
	for i := range a {
		d := int64(a[i]) - int64(b[i])
		diffs[i] = d
		if d != 0 {
			nonZero++
		}
	}
	return nonZero, diffs
}

// bodyHash 对单个实体的 4 个状态槽做与 internal/hash 相同的 FNV-1a，
// 用于给出"每组"的指纹差值。
func bodyHash(snap []int32, body int) uint64 {
	const offset = 14695981039346656037
	const prime = 1099511628211
	h := uint64(offset)
	for _, v := range snap[body*4 : body*4+4] {
		h ^= uint64(uint32(v))
		h *= prime
	}
	return h
}

// TestDifferentBodiesCommute 验证：不同实体的两条指令换序，末态不变。
func TestDifferentBodiesCommute(t *testing.T) {
	h1, s1 := runOrder(t, []Command{cmdA1, cmdB1}, 4)
	h2, s2 := runOrder(t, []Command{cmdB1, cmdA1}, 4)
	nz, _ := diffSlots(s1, s2)
	t.Logf("异实体换序: hash=%016x vs %016x, xor=%016x, 快照非零项=%d", h1, h2, h1^h2, nz)
	if h1 != h2 || nz != 0 {
		t.Fatalf("不同实体指令不可交换: hash xor=%016x 非零项=%d", h1^h2, nz)
	}
}

// TestSameBodyCommandsCommute 验证：同一实体的两条指令在本内核中同样可交换。
// Push 是纯定点加法，加法可交换，故"同实体不可交换"的前提不成立。
func TestSameBodyCommandsCommute(t *testing.T) {
	h1, s1 := runOrder(t, []Command{cmdA1, cmdA2}, 4)
	h2, s2 := runOrder(t, []Command{cmdA2, cmdA1}, 4)
	nz, _ := diffSlots(s1, s2)
	t.Logf("同实体换序: hash=%016x vs %016x, xor=%016x, 快照非零项=%d", h1, h2, h1^h2, nz)
	if h1 != h2 || nz != 0 {
		t.Fatalf("同一实体指令不可交换: hash xor=%016x 非零项=%d", h1^h2, nz)
	}
}

// TestGroupedCrossCheck 两组指令互测：
// 基线为按目标分组、组内保序的规范序列；
// 变体一交换 A 组组内顺序，变体二交换 B 组组内顺序，变体三为两组交错。
// 逐项比对末态哈希与每个实体的位置/速度，并分别给出两组的指纹差值。
func TestGroupedCrossCheck(t *testing.T) {
	groups, canonical := GroupCommands([]Command{cmdA1, cmdA2, cmdB1, cmdB2})
	if len(groups) != 2 {
		t.Fatalf("应分为 2 组, 实际 %d", len(groups))
	}

	variants := []struct {
		name  string
		order []Command
	}{
		{"baseline-grouped", canonical},
		{"swap-within-group-A", []Command{cmdA2, cmdA1, cmdB1, cmdB2}},
		{"swap-within-group-B", []Command{cmdA1, cmdA2, cmdB2, cmdB1}},
		{"interleaved", []Command{cmdB1, cmdA1, cmdB2, cmdA2}},
	}

	type result struct {
		hash uint64
		snap []int32
	}
	results := make(map[string]result, len(variants))
	for _, v := range variants {
		h, s := runOrder(t, v.order, 4)
		results[v.name] = result{h, s}
		t.Logf("%-22s hash=%016x", v.name, h)
	}

	base := results["baseline-grouped"]
	record := map[string]any{
		"options": orderOpt,
		"frames":  4,
		"groups": []map[string]any{
			{"body": 0, "commands": groups[0]},
			{"body": 1, "commands": groups[1]},
		},
		"variants": []map[string]any{},
		"diffs":    []map[string]any{},
	}
	for _, v := range variants {
		r := results[v.name]
		record["variants"] = append(record["variants"].([]map[string]any), map[string]any{
			"name":     v.name,
			"order":    v.order,
			"hash":     r.hash,
			"snapshot": r.snap,
		})
		if v.name == "baseline-grouped" {
			continue
		}
		nz, slotDiffs := diffSlots(base.snap, r.snap)
		// 两组各自的指纹差值（A 组=实体0, B 组=实体1）
		hashA0, hashA1 := bodyHash(base.snap, 0), bodyHash(r.snap, 0)
		hashB0, hashB1 := bodyHash(base.snap, 1), bodyHash(r.snap, 1)
		t.Logf("baseline vs %-20s 整体hash xor=%016x 差值=%d | A组(实体0)指纹 xor=%016x | B组(实体1)指纹 xor=%016x | 快照非零项=%d",
			v.name, base.hash^r.hash, int64(base.hash)-int64(r.hash), hashA0^hashA1, hashB0^hashB1, nz)
		record["diffs"] = append(record["diffs"].([]map[string]any), map[string]any{
			"pair":            "baseline-grouped vs " + v.name,
			"hashXor":         base.hash ^ r.hash,
			"groupA_hashXor":  hashA0 ^ hashA1,
			"groupB_hashXor":  hashB0 ^ hashB1,
			"slotDiffs":       slotDiffs,
			"nonZeroSlots":    nz,
			"slotLayout":      "每实体4槽: posX,posY,velX,velY; 实体0=槽0-3(A组), 实体1=槽4-7(B组)",
		})
		// 真实性质：本内核中组内换序不改变任何结果。
		if nz != 0 || base.hash != r.hash {
			t.Fatalf("%s 改变了末态: hash xor=%016x 非零项=%d", v.name, base.hash^r.hash, nz)
		}
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join("..", "artifacts", "grouping-record.json")
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("分组记录已写入 %s", out)
}

// TestGroupCanonicalStable 验证：凡保持组内相对顺序的排列（组的任意交错），
// 经分组后得到同一规范序列；而组内换序的排列会得到不同的规范序列，
// 但由可交换性，二者末态哈希仍相同。
func TestGroupCanonicalStable(t *testing.T) {
	_, want := GroupCommands([]Command{cmdA1, cmdA2, cmdB1, cmdB2})
	interleavings := [][]Command{
		{cmdB1, cmdA1, cmdB2, cmdA2},
		{cmdA1, cmdB1, cmdA2, cmdB2},
		{cmdB1, cmdB2, cmdA1, cmdA2},
	}
	for _, p := range interleavings {
		_, got := GroupCommands(p)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("交错排列应得到同一规范序列: %v vs %v", got, want)
			}
		}
	}
	// 组内换序 → 规范序列不同，但末态哈希相同（可交换性的直接推论）。
	_, swapped := GroupCommands([]Command{cmdA2, cmdA1, cmdB1, cmdB2})
	if swapped[0] == want[0] {
		t.Fatal("组内换序应产生不同的规范序列")
	}
	h1, _ := runOrder(t, want, 4)
	h2, _ := runOrder(t, swapped, 4)
	if h1 != h2 {
		t.Fatalf("规范序列不同但末态应相同: %016x vs %016x", h1, h2)
	}
}
