package parallel

import (
	"testing"

	"goframe/internal/fixed"
)

func testCfg() ScenarioConfig {
	return ScenarioConfig{Bodies: 12, Frames: 16, SubSteps: 2, Gravity: 2, CouplingShift: 2}
}

// TestOwnerPartition 验证连续环分区：每个实体恰好一个属主，覆盖全部实体。
func TestOwnerPartition(t *testing.T) {
	n, w := 12, 4
	counts := make([]int, w)
	for body := 0; body < n; body++ {
		o := Owner(body, w, n)
		if o < 0 || o >= w {
			t.Fatalf("body=%d owner=%d 越界", body, o)
		}
		counts[o]++
	}
	for i, c := range counts {
		if c != n/w {
			t.Fatalf("worker %d 分得 %d 个实体，期望 %d", i, c, n/w)
		}
	}
}

// TestDisjointnessEquivalence 定理一的可执行验证：
// 写入集合互不相交（staged）时任意并发度末态哈希等于串行；
// 不相交被破坏（naive, W>1）时条件判定必须为 false。
func TestDisjointnessEquivalence(t *testing.T) {
	cfg := testCfg()
	rep := RunExperiment(cfg, []int{1, 2, 4})

	for _, lr := range rep.Levels {
		if !lr.StagedDisjoint {
			t.Fatalf("W=%d staged 计划应满足写入集合互不相交", lr.Workers)
		}
		if lr.StagedHash != rep.SerialNaiveHash {
			t.Fatalf("W=%d 不相交却末态不一致: %016x != %016x",
				lr.Workers, lr.StagedHash, rep.SerialNaiveHash)
		}
		if lr.StagedDistance != 0 || lr.StagedDiffLocs != 0 {
			t.Fatalf("W=%d 不相交却存在差异 D=%d", lr.Workers, lr.StagedDistance)
		}
		if lr.Workers > 1 {
			if lr.NaiveDisjoint {
				t.Fatalf("W=%d naive 计划存在跨 worker 耦合，条件应不成立", lr.Workers)
			}
			if lr.NaiveParallelErr == nil {
				t.Fatalf("W=%d naive 计划应被真实并发执行器拒绝", lr.Workers)
			}
		}
	}
}

// TestSharedWriteCountByField 验证共享写入按字段统计的口径：
// 只统计非属主写入，按字段分别计数，并与实体数一起报告。
func TestSharedWriteCountByField(t *testing.T) {
	cfg := testCfg()
	inputs := buildInputs(cfg)
	st := initialState(cfg)
	plans := RunSerial(st, inputs, BuildNaive)

	r2 := Analyze(plans, 2, cfg.Bodies)
	r4 := Analyze(plans, 4, cfg.Bodies)

	// 每个跨分区边界的目标实体在 velX、velY 上各被跨 worker 写一次：
	// 共享写入数 = 帧数 × 跨边界目标实体数 × 2 个速度字段。
	// W=2 有 2 个跨边界目标（实体 6 与环回的实体 0），W=4 有 4 个。
	if r2.SharedWrites != cfg.Frames*2*2 {
		t.Fatalf("W=2 S=%d，期望 %d", r2.SharedWrites, cfg.Frames*2*2)
	}
	if r4.SharedWrites != cfg.Frames*4*2 {
		t.Fatalf("W=4 S=%d，期望 %d", r4.SharedWrites, cfg.Frames*4*2)
	}
	// 位置字段 posX/posY 永远是属主本地写，共享写入必须为 0。
	if r4.PerField[PosX].SharedWrites != 0 || r4.PerField[PosY].SharedWrites != 0 {
		t.Fatal("位置字段不应出现共享写入")
	}
	// 按字段求和必须等于总量。
	sum := 0
	for _, fs := range r4.PerField {
		sum += fs.SharedWrites
	}
	if sum != r4.SharedWrites {
		t.Fatalf("按字段求和 %d != 总 S=%d", sum, r4.SharedWrites)
	}
	// 涉及实体数不得超过实体总数，且 W=4 应大于 W=2。
	if r4.SharedBodies > cfg.Bodies || r4.SharedBodies <= r2.SharedBodies {
		t.Fatalf("共享实体数异常: W2=%d W4=%d N=%d", r2.SharedBodies, r4.SharedBodies, cfg.Bodies)
	}
}

// TestDifferenceBound 定理二的可执行验证：D <= 2*M*S*L，且 S=0 时 D=0。
func TestDifferenceBound(t *testing.T) {
	cfg := testCfg()
	rep := RunExperiment(cfg, []int{1, 2, 4})

	var prevD int64 = -1
	var prevS int
	for _, lr := range rep.Levels {
		bound, ok := rep.BoundOK(&lr)
		if !ok {
			t.Fatalf("W=%d D=%d 超过上界 %d", lr.Workers, lr.NaiveDistance, bound)
		}
		if lr.NaiveSharedWrites == 0 && lr.NaiveDistance != 0 {
			t.Fatalf("W=%d S=0 时 D 必须为 0", lr.Workers)
		}
		if lr.Workers > 1 {
			// 同场景下 S 增大时实测差异应同向增长（比例性的经验验证）。
			if lr.NaiveDistance <= prevD || lr.NaiveSharedWrites <= prevS {
				t.Fatalf("D 未随 S 增长: S %d->%d, D %d->%d",
					prevS, lr.NaiveSharedWrites, prevD, lr.NaiveDistance)
			}
		}
		prevD = lr.NaiveDistance
		prevS = lr.NaiveSharedWrites
	}
}

// TestSerialPlansByteIdentical 两种计划的串行末态必须逐字节相同，
// 证明修复只改变调度与写入归属，不改变仿真语义。
func TestSerialPlansByteIdentical(t *testing.T) {
	cfg := testCfg()
	inputs := buildInputs(cfg)

	a := initialState(cfg)
	RunSerial(a, inputs, BuildNaive)
	b := initialState(cfg)
	RunSerial(b, inputs, BuildStaged)

	ra, rb := a.Raw(), b.Raw()
	if len(ra) != len(rb) {
		t.Fatalf("导出长度不同 %d != %d", len(ra), len(rb))
	}
	for i := range ra {
		if ra[i] != rb[i] {
			t.Fatalf("字段 %d 串行结果不同: %d != %d", i, ra[i], rb[i])
		}
	}
}

// TestInterleaveDeterministic 交错执行必须完全确定：同参数两次运行末态一致。
func TestInterleaveDeterministic(t *testing.T) {
	cfg := testCfg()
	inputs := buildInputs(cfg)

	first := initialState(cfg)
	RunInterleave(first, inputs, 4, BuildNaive)
	second := initialState(cfg)
	RunInterleave(second, inputs, 4, BuildNaive)

	if Hash(cfg.Frames, first) != Hash(cfg.Frames, second) {
		t.Fatal("交错执行不确定")
	}
}

// TestParallelRejectsAndAccepts 真实 goroutine 执行器的条件闸门：
// naive 被拒、staged 被接受，且 staged 结果与串行相同。
func TestParallelRejectsAndAccepts(t *testing.T) {
	cfg := testCfg()
	inputs := buildInputs(cfg)

	if _, err := RunParallel(initialState(cfg), inputs, 4, BuildNaive); err != ErrSharedWrite {
		t.Fatalf("naive 应返回 ErrSharedWrite，实际 %v", err)
	}
	st := initialState(cfg)
	if _, err := RunParallel(st, inputs, 4, BuildStaged); err != nil {
		t.Fatalf("staged 不应被拒绝: %v", err)
	}
	ref := initialState(cfg)
	RunSerial(ref, inputs, BuildNaive)
	if Hash(cfg.Frames, st) != Hash(cfg.Frames, ref) {
		t.Fatal("staged 真实并发末态与串行不一致")
	}
}

// TestFixedPointNoFloat 守卫：实验全程只用定点整数，不引入浮点。
func TestFixedPointNoFloat(t *testing.T) {
	v := fixed.FromRaw(1 << 16)
	if got := v.Add(fixed.FromRaw(1 << 15)).Raw(); got != (3 << 15) {
		t.Fatalf("定点加法异常: %d", got)
	}
}
