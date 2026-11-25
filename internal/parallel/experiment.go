package parallel

import "goframe/internal/fixed"

// ScenarioConfig 是并发对拍实验的确定性场景参数。
type ScenarioConfig struct {
	Bodies        int  // 实体数 N
	Frames        int  // 帧数 T
	SubSteps      int  // 每帧子步数
	Gravity       int  // 重力（Q16.16 原始值）
	CouplingShift uint // 环上耦合冲量的右移位数
}

// LevelResult 是单个并发度（worker 数）下的完整对拍结果。
type LevelResult struct {
	Workers int

	// 朴素计划（含共享写入的错误写法）
	NaiveDisjoint     bool // 条件判定：写入集合是否互不相交
	NaiveSharedWrites int  // 共享写入次数 S（按字段位置统计）
	NaiveSharedBodies int  // 涉及共享写入的实体数
	NaivePerField     [numPhysical]FieldStat
	NaiveOutboxShared int
	NaiveTotalWrites  int
	NaiveHash         uint64 // 确定性交错执行的末态哈希
	NaiveDistance     int64  // 与串行末态的 L1 原始单位距离 D
	NaiveDiffLocs     int    // 取值不同的字段位置数
	NaiveParallelErr  error  // 真实 goroutine 执行的结果（不满足条件时为 ErrSharedWrite）

	// 两阶段计划（修复写法）
	StagedDisjoint     bool
	StagedSharedWrites int
	StagedSharedBodies int
	StagedPerField     [numPhysical]FieldStat
	StagedHash         uint64 // 真实 goroutine 并发执行的末态哈希
	StagedDistance     int64
	StagedDiffLocs     int
	StagedParallelErr  error
}

// ExperimentReport 是整份实验报告。
type ExperimentReport struct {
	Cfg ScenarioConfig

	SerialNaiveHash  uint64 // 串行基准（朴素计划）
	SerialStagedHash uint64 // 串行基准（两阶段计划，应与前者相同）

	MaxDelta int64 // 全部操作增量绝对值的上界 M
	MaxDepth int   // 单个字段位置在整段运行中的最大写入次数 L

	Levels []LevelResult
}

// initialState 构造确定性初态：位置与速度按实体编号给出非零初值，
// 保证耦合冲量从第一帧起就有非零增量。
func initialState(cfg ScenarioConfig) *State {
	s := NewState(cfg.Bodies, fixed.FromRaw(cfg.Gravity))
	for i := 0; i < cfg.Bodies; i++ {
		s.posX[i] = fixed.FromRaw((i + 1) * (1 << 10))
		s.posY[i] = fixed.FromRaw((i + 2) * (1 << 9))
		s.velX[i] = fixed.FromRaw(((i+1)*5 + 3) << 12)
		s.velY[i] = fixed.FromRaw(((i+2)*7 + 1) << 11)
	}
	return s
}

// buildInputs 生成确定性帧输入：每帧两个外部冲量 + 全体环上耦合。
func buildInputs(cfg ScenarioConfig) []FrameInput {
	in := make([]FrameInput, cfg.Frames)
	for t := 0; t < cfg.Frames; t++ {
		in[t] = FrameInput{
			SubSteps: cfg.SubSteps,
			Pushes: []Push{
				{Body: t % cfg.Bodies, DX: fixed.FromRaw(1 << 14), DY: fixed.FromRaw(0)},
				{Body: (t*3 + 1) % cfg.Bodies, DX: fixed.FromRaw(0), DY: fixed.FromRaw(-(1 << 13))},
			},
			Coupling: make([]Coupling, cfg.Bodies),
		}
		for i := 0; i < cfg.Bodies; i++ {
			in[t].Coupling[i] = Coupling{Body: i, Shift: cfg.CouplingShift}
		}
	}
	return in
}

// maxLocDepth 统计计划序列中单个字段位置的最大写入次数 L。
func maxLocDepth(plans []FramePlan) int {
	depth := make(map[Loc]int)
	max := 0
	for _, p := range plans {
		for _, phase := range p.Phases {
			for _, op := range phase {
				depth[op.Loc]++
				if depth[op.Loc] > max {
					max = depth[op.Loc]
				}
			}
		}
	}
	return max
}

// RunExperiment 跑完整对拍实验。workers 给出三档并发度；
// 同一确定性场景在串行、确定性交错、真实 goroutine 三种执行方式下对拍。
func RunExperiment(cfg ScenarioConfig, workers []int) *ExperimentReport {
	inputs := buildInputs(cfg)

	// 串行基准：朴素计划。
	serialRef := initialState(cfg)
	naivePlans := RunSerial(serialRef, inputs, BuildNaive)
	serialNaiveFinal := serialRef.Clone()

	// 串行基准：两阶段计划（语义必须与朴素计划逐字节相同）。
	serialStagedState := initialState(cfg)
	stagedPlans := RunSerial(serialStagedState, inputs, BuildStaged)

	rep := &ExperimentReport{
		Cfg:              cfg,
		SerialNaiveHash:  Hash(cfg.Frames, serialNaiveFinal),
		SerialStagedHash: Hash(cfg.Frames, serialStagedState),
		MaxDelta:         MaxDelta(initialState(cfg), naivePlans),
		MaxDepth:         maxLocDepth(naivePlans),
	}

	for _, w := range workers {
		lr := LevelResult{Workers: w}

		// ---- 朴素计划：条件判定 + 确定性交错复现分歧 + 真实并发应被拒绝 ----
		naiveReport := Analyze(naivePlans, w, cfg.Bodies)
		lr.NaiveDisjoint = naiveReport.Disjoint
		lr.NaiveSharedWrites = naiveReport.SharedWrites
		lr.NaiveSharedBodies = naiveReport.SharedBodies
		lr.NaivePerField = naiveReport.PerField
		lr.NaiveOutboxShared = naiveReport.OutboxShared
		lr.NaiveTotalWrites = naiveReport.TotalWrites

		ilState := initialState(cfg)
		RunInterleave(ilState, inputs, w, BuildNaive)
		lr.NaiveHash = Hash(cfg.Frames, ilState)
		lr.NaiveDistance, lr.NaiveDiffLocs = Distance(serialNaiveFinal, ilState)

		_, lr.NaiveParallelErr = RunParallel(initialState(cfg), inputs, w, BuildNaive)

		// ---- 两阶段计划：条件判定 + 真实并发末态必须等于串行 ----
		stagedReport := Analyze(stagedPlans, w, cfg.Bodies)
		lr.StagedDisjoint = stagedReport.Disjoint
		lr.StagedSharedWrites = stagedReport.SharedWrites
		lr.StagedSharedBodies = stagedReport.SharedBodies
		lr.StagedPerField = stagedReport.PerField

		plState := initialState(cfg)
		_, err := RunParallel(plState, inputs, w, BuildStaged)
		lr.StagedParallelErr = err
		lr.StagedHash = Hash(cfg.Frames, plState)
		lr.StagedDistance, lr.StagedDiffLocs = Distance(serialNaiveFinal, plState)

		rep.Levels = append(rep.Levels, lr)
	}
	return rep
}

// BoundOK 判定差异上界 D <= 2*M*S*L 是否成立。
//
// M 为单操作增量上界，S 为共享写入次数（按字段），
// L 为单位置最大写入次数（场景结构常数，与 S 无关）。
// 因子 2 来自两次执行中同一操作增量之差 |δc−δs| <= 2M。
func (r *ExperimentReport) BoundOK(lr *LevelResult) (bound int64, ok bool) {
	bound = 2 * r.MaxDelta * int64(lr.NaiveSharedWrites) * int64(r.MaxDepth)
	return bound, lr.NaiveDistance <= bound
}
