package drift

import "testing"

// TestThreeCasesNormalizedReplayMatchesDirect 断言三份快照：
// naive 在检查点即漂移；规范化后不存在漂移帧，逐帧哈希全等；公式残差为 0。
func TestThreeCasesNormalizedReplayMatchesDirect(t *testing.T) {
	for _, spec := range DefaultSpecs() {
		cr, err := RunCase(spec)
		if err != nil {
			t.Fatalf("%s: %v", spec.ID, err)
		}
		if cr.DriftStartTickInclCheckpoint == nil ||
			*cr.DriftStartTickInclCheckpoint != spec.Checkpoint {
			t.Fatalf("%s: naive 漂移应在检查点 %d", spec.ID, spec.Checkpoint)
		}
		if cr.NormalizedDriftStartTick != nil {
			t.Fatalf("%s: 规范化后不应有漂移帧, got tick=%d", spec.ID, *cr.NormalizedDriftStartTick)
		}
		for _, f := range cr.Frames {
			if !f.NormalizedHashEqual {
				t.Fatalf("%s f=%d: 规范化哈希不一致 %s vs %s",
					spec.ID, f.F, f.NormalizedHash, f.DirectHash)
			}
			if f.NaiveHashEqual {
				t.Fatalf("%s f=%d: naive 哈希不应等于直跑", spec.ID, f.F)
			}
		}
		if !cr.FormulaExactAllFrames || cr.FormulaMaxAbsResidual != 0 {
			t.Fatalf("%s: 公式应逐帧逐字段精确, exact=%v residual=%d",
				spec.ID, cr.FormulaExactAllFrames, cr.FormulaMaxAbsResidual)
		}
	}
}

// TestCase1ExactValues 锁定 case1 的具体哈希与偏差量，防止回归。
func TestCase1ExactValues(t *testing.T) {
	cr, err := RunCase(DefaultSpecs()[0])
	if err != nil {
		t.Fatal(err)
	}
	if cr.CheckpointHashDirect != "e6374811361def73" {
		t.Fatalf("direct C hash = %s", cr.CheckpointHashDirect)
	}
	if cr.NaiveCheckpointHash != "4816b89b14052533" {
		t.Fatalf("naive C hash = %s", cr.NaiveCheckpointHash)
	}
	if cr.NormalizedCheckpointHash != cr.CheckpointHashDirect {
		t.Fatal("normalized C hash mismatch")
	}
	if cr.DeviationAtDriftStartRaw != 43644672 {
		t.Fatalf("start dev = %d", cr.DeviationAtDriftStartRaw)
	}
	if cr.DeviationAtEndRaw != 99327744 {
		t.Fatalf("end dev = %d", cr.DeviationAtEndRaw)
	}
	if cr.Frames[1].Dx0Raw != -12337662 {
		t.Fatalf("dx0 f1 = %d", cr.Frames[1].Dx0Raw)
	}
	if cr.DirectFinalHash != "749cd25c92530a3b" ||
		cr.NormalizedFinalHash != "749cd25c92530a3b" ||
		cr.NaiveFinalHash != "1f922e87f93a877b" {
		t.Fatalf("final hashes changed: %s %s %s",
			cr.DirectFinalHash, cr.NormalizedFinalHash, cr.NaiveFinalHash)
	}
}

// TestFactorialOnlyBothFixesRecover 2×2 归因：只有「大端 + 反转」同时修对才还原。
func TestFactorialOnlyBothFixesRecover(t *testing.T) {
	cr, err := RunCase(DefaultSpecs()[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(cr.Factorial) != 4 {
		t.Fatalf("factorial rows=%d", len(cr.Factorial))
	}
	for i, r := range cr.Factorial {
		wantRecover := i == 3
		if r.RecoversExactState != wantRecover {
			t.Fatalf("%s recovers=%v want %v", r.Mode, r.RecoversExactState, wantRecover)
		}
		if r.AllHashesEqualDirect != wantRecover {
			t.Fatalf("%s allEqual=%v want %v", r.Mode, r.AllHashesEqualDirect, wantRecover)
		}
		if wantRecover {
			if r.FirstDiffF != nil || r.DevAtLoadRaw != 0 || r.DevAtEndRaw != 0 {
				t.Fatalf("normalized row should be perfect: %+v", r)
			}
		} else if r.FirstDiffTick == nil || *r.FirstDiffTick != cr.Spec.Checkpoint {
			t.Fatalf("%s 应在检查点 %d 即不同", r.Mode, cr.Spec.Checkpoint)
		}
	}
}

// TestLatentOnsetAfterDozensOfFrames 对应症状：整数 X 前 31 续跑帧相同，
// 第 32 帧（全局 tick=80）首次差 1 整单位（65536 raw）。
func TestLatentOnsetAfterDozensOfFrames(t *testing.T) {
	lat, err := RunLatent()
	if err != nil {
		t.Fatal(err)
	}
	if !lat.NormalizedAllHashEqual {
		t.Fatal("规范化后 latent 全部哈希应与直跑一致")
	}
	if lat.XWholeUnitOnsetF != 32 || lat.XWholeUnitOnsetTick != 80 {
		t.Fatalf("onset f=%d tick=%d", lat.XWholeUnitOnsetF, lat.XWholeUnitOnsetTick)
	}
	if lat.OnsetDeltaXRaw != 65536 || lat.OnsetDeltaXWholeUnits != 1 {
		t.Fatalf("onset dx=%d units=%d", lat.OnsetDeltaXRaw, lat.OnsetDeltaXWholeUnits)
	}
	for f := 0; f < 32; f++ {
		if !lat.Frames[f].XWholeUnitEqual {
			t.Fatalf("f=%d 整数 X 不应不同", f)
		}
	}
	if lat.Frames[32].XWholeUnitEqual {
		t.Fatal("f=32 整数 X 应首次不同")
	}
}

// TestCalibrationFormula 验证闭式 Δx_b=f·N、Σ=fN²/4 网格全部精确。
func TestCalibrationFormula(t *testing.T) {
	grid, err := BuildCalibrationGrid()
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) != 5*8 {
		t.Fatalf("grid rows=%d", len(grid))
	}
	for _, r := range grid {
		if !r.Exact {
			t.Fatalf("N=%d f=%d not exact: dx %d/%d total %d/%d",
				r.N, r.F, r.MeasDxBody0Raw, r.PredDxRaw, r.MeasTotalXRaw, r.PredTotalXRaw)
		}
	}
	// 抽查 N=16(B=4), f=8：Δx0=128，Σ=4*128=512=fN²/4=8*256/4。
	for _, r := range grid {
		if r.N == 16 && r.F == 8 {
			if r.MeasDxBody0Raw != 128 || r.MeasTotalXRaw != 512 {
				t.Fatalf("spot row: %+v", r)
			}
			if r.WholeUnitOnsetF != 4096 {
				t.Fatalf("onset ceil(65536/16)=%d", r.WholeUnitOnsetF)
			}
		}
	}
}

// TestSubStepsFormulaFactorQ 证明每帧 S 子步时斜率因子是 q=f·S。
func TestSubStepsFormulaFactorQ(t *testing.T) {
	spec := Spec{
		ID: "sub4", Bodies: 3, Gravity: 2, SubSteps: 4,
		Checkpoint: 10, Extra: 8, PushRaw: 1 << 15,
	}
	cr, err := RunCase(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !cr.FormulaExactAllFrames || cr.FormulaMaxAbsResidual != 0 {
		t.Fatalf("S=4 时公式仍应精确（q=f·S）, residual=%d", cr.FormulaMaxAbsResidual)
	}
	if cr.NormalizedDriftStartTick != nil {
		t.Fatal("规范化后不应有漂移帧")
	}
}
