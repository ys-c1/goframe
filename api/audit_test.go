package api

import (
	"testing"
)

// auditScenarios 复用对外导出的三组输入，保证测试与留档一致。
func auditScenarios() []Scenario { return AuditScenarios() }

func TestAuditScenarios(t *testing.T) {
	results, err := RunAudit(auditScenarios())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("应有三组结果，实际 %d", len(results))
	}
	for _, r := range results {
		if !r.SnapEqual {
			t.Errorf("%s: 归并后状态与原序不同", r.Name)
		}
		if r.HashMerged != r.HashOrig {
			t.Errorf("%s: 归并指纹 %016x != 原序 %016x", r.Name, r.HashMerged, r.HashOrig)
		}
	}
	// A: 同实体同分量两条，丢弃正好少一份 1<<16。
	if results[0].LostX != 1<<16 || results[0].LostY != 0 {
		t.Fatalf("A 组丢量应为 (1<<16,0)，实际 (%d,%d)", results[0].LostX, results[0].LostY)
	}
	// B: 组 0 三条各 1<<15（丢两份 2<<15），组 1 两条各 -1<<16（丢一份 -1<<16）。
	if results[1].LostX != 2<<15 || results[1].LostY != -1<<16 {
		t.Fatalf("B 组丢量应为 (%d,%d)，实际 (%d,%d)", 2<<15, -1<<16, results[1].LostX, results[1].LostY)
	}
	// C: 零冲量重复，丢弃无损。
	if results[2].LostX != 0 || results[2].LostY != 0 {
		t.Fatalf("C 零冲量重复不应丢量: X=%d Y=%d", results[2].LostX, results[2].LostY)
	}
	if results[2].HashDropped != results[2].HashOrig {
		t.Fatal("C 组丢弃后指纹应与原序相同")
	}
	if !results[2].SnapEqual || results[2].HashMerged != results[2].HashOrig {
		t.Fatal("C 组归并应与原序一致")
	}
	// A、B 的丢弃必须改变状态。
	for _, i := range []int{0, 1} {
		if results[i].HashDropped == results[i].HashOrig {
			t.Fatalf("第 %d 组丢弃后指纹应与原序不同", i)
		}
	}
	// B 中帧号不同的那条不算重复：组数为 2。
	if len(results[1].Lost) != 2 {
		t.Fatalf("B 应有两组丢量，实际 %+v", results[1].Lost)
	}
}

func TestAuditDeterministic(t *testing.T) {
	// 同输入两次审计逐字节相同，证明审计本身可重放。
	a, err := RunAudit(auditScenarios())
	if err != nil {
		t.Fatal(err)
	}
	b, err := RunAudit(auditScenarios())
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatal("行数不同")
	}
	for i := range a {
		if a[i].HashOrig != b[i].HashOrig || a[i].HashMerged != b[i].HashMerged || a[i].HashDropped != b[i].HashDropped {
			t.Fatalf("第 %d 组两次审计指纹不同", i)
		}
	}
}

func TestImpulseLossFormula(t *testing.T) {
	// 丢量 = (m-1)d，模 2^32。取 d 接近回绕边界。
	for _, tc := range []struct{ d, m int }{
		{1 << 15, 3}, {-1 << 31, 2}, {1<<31 - 1, 2}, {7, 1000}, {0, 9},
	} {
		cmds := make([]Command, tc.m)
		for i := range cmds {
			cmds[i] = Command{Tick: 0, Body: 0, PushX: tc.d}
		}
		if !hasDuplicate(cmds) {
			t.Fatalf("m=%d 应识别为重复", tc.m)
		}
		loss := DropLoss(cmds)
		if tc.d == 0 {
			if len(loss) != 1 || loss[0].Count != tc.m || loss[0].LostX != 0 {
				t.Fatalf("零冲量重复应列为 1 组且丢量为零: %+v", loss)
			}
			continue
		}
		if len(loss) != 1 {
			t.Fatalf("应有一组丢量: %+v", loss)
		}
		want := int(int32(int64(tc.d) * int64(tc.m-1)))
		if loss[0].LostX != want {
			t.Fatalf("d=%d m=%d 丢量 %d 期望 %d", tc.d, tc.m, loss[0].LostX, want)
		}
	}
}

func hasDuplicate(cmds []Command) bool { return len(DropLoss(cmds)) > 0 }
