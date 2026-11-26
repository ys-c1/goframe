package snap

import (
	"errors"
	"math"
	"testing"
)

func testIndex(t *testing.T, bodies int) *Index {
	t.Helper()
	x, err := NewIndex(bodies)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// TestSufficiencyIDPlusOld 验证充分性：编号唯一 + 旧值真实时，
// 差分一定能唯一还原，且逐字段比较全部相等。
func TestSufficiencyIDPlusOld(t *testing.T) {
	x := testIndex(t, 2) // N=8
	oldV := []int32{10, 20, 30, 40, 10, 60, 70, 80}
	newV := append([]int32(nil), oldV...)
	newV[1] = 21
	newV[4] = 99 // oldV[4]==10 与 oldV[0] 重复，专门考验“只靠旧值”

	d, err := Build(x, oldV, newV)
	if err != nil {
		t.Fatal(err)
	}
	if d.ChangeCount() != 2 {
		t.Fatalf("k=%d, 应为 2", d.ChangeCount())
	}
	got, err := d.RestoreValues(x, newV)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Compare(x, oldV, got)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.AllEqual || rep.DifferentCount != 0 || rep.Total != x.N() {
		t.Fatalf("逐字段还原失败: %s", rep.SummaryLine())
	}

	// 两条差分记录都能用“编号+旧值”在旧全量快照中唯一命中。
	full, err := NewSnapshot(x, 3, oldV)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Entries {
		idx, err := full.LocateChanged(e.ID, e.Old)
		if err != nil {
			t.Fatalf("复合定位失败 (%d,%d): %v", e.ID, e.Old, err)
		}
		if FieldID(idx) != e.ID {
			t.Fatalf("定位槽位 %d != 编号 %d", idx, e.ID)
		}
	}
}

// TestNecessityOldValueAloneAmbiguous 验证必要性的一半：
// 若定位只靠旧值，重复值会让字段无法唯一确定，还原不唯一。
func TestNecessityOldValueAloneAmbiguous(t *testing.T) {
	x := testIndex(t, 2)
	// 字段 0 与字段 4 的旧值都是 10。
	oldV := []int32{10, 20, 30, 40, 10, 60, 70, 80}
	full, err := NewSnapshot(x, 0, oldV)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := full.LocateByOldValue(10); !errors.Is(err, ErrNotLocatable) {
		t.Fatalf("旧值 10 重复时应无法唯一定位, got %v", err)
	}
	// 同一个旧值 10，写回字段 0 或字段 4 会得到两个不同的旧状态：
	// 这就是“不可唯一还原”的具体构造。
	candidateA := []int32{10, 0, 0, 0, 0, 0, 0, 0}
	candidateB := []int32{0, 0, 0, 0, 10, 0, 0, 0}
	if ok, _ := Compare(x, candidateA, candidateB); ok.AllEqual {
		t.Fatal("两个候选旧状态本应不同")
	}
	// 加上编号后，(0,10) 与 (4,10) 立即区分开。
	if _, err := full.LocateChanged(0, 10); err != nil {
		t.Fatalf("编号+旧值应可定位字段0: %v", err)
	}
	if _, err := full.LocateChanged(4, 10); err != nil {
		t.Fatalf("编号+旧值应可定位字段4: %v", err)
	}
}

// TestNecessityWrongOldAndDuplicateID 验证必要性的另一半：
// 旧值错误（不是真值）或同编号出现多条，都不能给出唯一正确还原。
func TestNecessityWrongOldAndDuplicateID(t *testing.T) {
	x := testIndex(t, 1)
	full, _ := NewSnapshot(x, 0, []int32{1, 2, 3, 4})
	if _, err := full.LocateChanged(0, 999); !errors.Is(err, ErrNotLocatable) {
		t.Fatalf("旧值造假应定位失败, got %v", err)
	}
	bad := &Delta{N: x.N(), Entries: []Entry{{ID: 0, Old: 1}, {ID: 0, Old: 1}}}
	if err := bad.Validate(x.N()); !errors.Is(err, ErrNotLocatable) {
		t.Fatalf("同编号重复条目应判为不可唯一, got %v", err)
	}
	outOfRange := &Delta{N: x.N(), Entries: []Entry{{ID: FieldID(x.N()), Old: 1}}}
	if err := outOfRange.Validate(x.N()); !errors.Is(err, ErrBadFieldID) {
		t.Fatalf("编号越界应报错, got %v", err)
	}
}

// TestMissingChangedEntryCannotRestore 验证“每个被改字段都必须有条目”：
// 漏掉改动字段时，逐字段比对必须抓到该字段不一致。
func TestMissingChangedEntryCannotRestore(t *testing.T) {
	x := testIndex(t, 1)
	oldV := []int32{1, 2, 3, 4}
	newV := []int32{1, 22, 3, 44}
	// 故意只记录字段 1，漏掉字段 3。
	d := &Delta{N: x.N(), Entries: []Entry{{ID: 1, Old: oldV[1]}}}
	got, err := d.RestoreValues(x, newV)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Compare(x, oldV, got)
	if err != nil {
		t.Fatal(err)
	}
	if rep.AllEqual || rep.DifferentCount != 1 {
		t.Fatalf("漏掉字段3后应恰好 1 项不一致, got %s", rep.SummaryLine())
	}
	if ids := rep.MismatchIDs(); len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("不一致编号应为 [3], got %v", ids)
	}
}

// TestBoundAtMinAndMax 按题目要求，在改动量最小档 k=1 与最大档 k=N
// 各验一次体积上界；同时覆盖中档与 k=0 边界。
func TestBoundAtMinAndMax(t *testing.T) {
	const bodies = 8 // 与 README/默认规模一致：N=32
	x := testIndex(t, bodies)
	N := x.N()
	if N != 32 {
		t.Fatalf("N=%d, 应为 32", N)
	}
	F := FullSize(N)
	if F != 138 {
		t.Fatalf("全量体积=%d, 应为 138", F)
	}
	upperB := RatioUpperBound(N)
	if math.Abs(upperB-34.0/23.0) > 1e-12 {
		t.Fatalf("统一比值上界=%v, 应为 34/23", upperB)
	}

	cases := []struct {
		name string
		k    int
	}{
		{"zero", 0},
		{"min", 1},
		{"mid", N / 2},
		{"max", N},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldV := make([]int32, N)
			for i := range oldV {
				oldV[i] = int32(i*7 + 1)
			}
			newV := append([]int32(nil), oldV...)
			for i := 0; i < tc.k; i++ {
				newV[i] += 100000 // 保证不同
			}
			d, err := Build(x, oldV, newV)
			if err != nil {
				t.Fatal(err)
			}
			if d.ChangeCount() != tc.k {
				t.Fatalf("实际改动 k=%d, 构造 k=%d", d.ChangeCount(), tc.k)
			}
			codec, err := d.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			// 体积精确等于该档上界（定宽编码）。
			wantSize := DeltaSizeBound(N, tc.k)
			if len(codec) != wantSize || len(codec) != DeltaSize(N, tc.k) {
				t.Fatalf("差分体积=%d, 上界/期望=%d", len(codec), wantSize)
			}
			// 比值不超过统一上界 B(N)。
			r := float64(len(codec)) / float64(F)
			if r > upperB+1e-12 {
				t.Fatalf("k=%d 比值 %.6f 超过上界 %.6f", tc.k, r, upperB)
			}
			// 该档比值上界公式也必须取到等号。
			if math.Abs(r-RatioBoundAtK(N, tc.k)) > 1e-12 {
				t.Fatalf("k=%d 比值 %.12f != 该档上界 %.12f", tc.k, r, RatioBoundAtK(N, tc.k))
			}
			// 还原后逐字段全等。
			got, err := d.RestoreValues(x, newV)
			if err != nil {
				t.Fatal(err)
			}
			rep, err := Compare(x, oldV, got)
			if err != nil || !rep.AllEqual {
				t.Fatalf("k=%d 还原不一致: %v %s", tc.k, err, rep.SummaryLine())
			}
		})
	}

	// 最小档与最大档的显式断言（题目点名两档都要验）。
	minBytes := DeltaSizeBound(N, 1)
	maxBytes := DeltaSizeBound(N, N)
	if minBytes != 18 {
		t.Fatalf("最小档上界=%d, 应为 12+6=18", minBytes)
	}
	if maxBytes != 204 {
		t.Fatalf("最大档上界=%d, 应为 12+192=204", maxBytes)
	}
	if r := float64(minBytes) / float64(F); r > upperB {
		t.Fatal("最小档违反统一上界")
	}
	if r := float64(maxBytes) / float64(F); math.Abs(r-upperB) > 1e-12 {
		t.Fatal("最大档应恰好顶到统一上界（紧上界）")
	}
}

// TestCodecRoundTrip 验证二进制编解码往返后差分与快照一致，且长度严格。
func TestCodecRoundTrip(t *testing.T) {
	x := testIndex(t, 3)
	oldV := []int32{1, -2, 3, 4, 5, 6, -7, 8, 9, 10, 11, 12}
	newV := append([]int32(nil), oldV...)
	newV[0] = 100
	newV[11] = -1

	full, err := NewSnapshot(x, 42, oldV)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := full.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(fb) != FullSize(x.N()) {
		t.Fatalf("full bytes=%d want %d", len(fb), FullSize(x.N()))
	}
	f2, err := UnmarshalSnapshot(fb)
	if err != nil {
		t.Fatal(err)
	}
	if rep, _ := Compare(x, oldV, f2.Values); !rep.AllEqual || f2.Tick != 42 {
		t.Fatal("全量快照编解码往返不一致")
	}

	d, _ := Build(x, oldV, newV)
	d.TickBefore = 41
	db, err := d.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := UnmarshalDelta(db)
	if err != nil {
		t.Fatal(err)
	}
	if d2.TickBefore != 41 || len(d2.Entries) != len(d.Entries) {
		t.Fatal("差分元数据往返不一致")
	}
	got, err := d2.RestoreValues(x, newV)
	if err != nil {
		t.Fatal(err)
	}
	if rep, _ := Compare(x, oldV, got); !rep.AllEqual {
		t.Fatal("解码后的差分还原失败")
	}

	// 损坏/截断必须报错而不是静默还原。
	if _, err := UnmarshalDelta(db[:len(db)-1]); err == nil {
		t.Fatal("截断差分应报错")
	}
	bad := append([]byte(nil), db...)
	bad[0] ^= 0xFF
	if _, err := UnmarshalDelta(bad); err == nil {
		t.Fatal("魔数损坏应报错")
	}
}
