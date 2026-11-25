package api

// 本文件是本仓库对「同一 tick 收到两条相同指令」问题的量化审计入口。
// 结论与推导见 docs/dedup.md；这里只做可复现的跑数与留档。

import "fmt"

// Scenario 是一组待审计的输入。
type Scenario struct {
	Name string
	Opts Options
	Cmds []Command
}

// ScenarioResult 是一组输入在三种处理方式下的结果与判定，字段对应留档的三栏。
type ScenarioResult struct {
	Name        string
	HashOrig    uint64 // 原序逐条执行
	HashMerged  uint64 // 归并（把出现次数折进冲量）
	HashDropped uint64 // 只留一条、丢弃其余
	SnapEqual   bool   // 归并与原序的状态切片逐项相同
	HasConflict bool   // 是否存在同实体同字段的两条指令（字段交集非空）
	Lost        []ImpulseLoss
	LostX       int // 全部丢弃组丢失的 X 冲量，按 int32 回绕
	LostY       int
	Verdict     string
}

// RunAudit 跑全部场景，逐组返回三栏数据。
func RunAudit(scenarios []Scenario) ([]ScenarioResult, error) {
	out := make([]ScenarioResult, 0, len(scenarios))
	for _, sc := range scenarios {
		if err := sc.Opts.validate(); err != nil {
			return nil, err
		}
		wOrig, err := New(sc.Opts)
		if err != nil {
			return nil, err
		}
		if err := wOrig.Step(sc.Cmds); err != nil {
			return nil, err
		}
		wMerged, _ := New(sc.Opts)
		if err := wMerged.Step(DedupCommands(sc.Cmds)); err != nil {
			return nil, err
		}
		wDropped, _ := New(sc.Opts)
		if err := wDropped.Step(dropDuplicates(sc.Cmds)); err != nil {
			return nil, err
		}

		res := ScenarioResult{
			Name:        sc.Name,
			HashOrig:    wOrig.Hash(),
			HashMerged:  wMerged.Hash(),
			HashDropped: wDropped.Hash(),
			SnapEqual:   snapshotEqual(wOrig.Snapshot(), wMerged.Snapshot()),
			HasConflict: hasConflict(sc.Cmds),
			Lost:        DropLoss(sc.Cmds),
		}
		for _, l := range res.Lost {
			res.LostX = int(int32(int64(res.LostX) + int64(l.LostX)))
			res.LostY = int(int32(int64(res.LostY) + int64(l.LostY)))
		}
		switch {
		case !res.SnapEqual:
			res.Verdict = "归并仍失真"
		case len(res.Lost) == 0:
			res.Verdict = "无重复，归并即原序"
		case res.LostX == 0 && res.LostY == 0:
			res.Verdict = "重复但总丢量为零，丢弃无损"
		default:
			res.Verdict = "重复且非零冲量，丢弃丢量，须归并"
		}
		out = append(out, res)
	}
	return out, nil
}

// AuditScenarios 返回对外留档用的三组输入。
func AuditScenarios() []Scenario {
	return []Scenario{
		{
			Name: "A 同一实体两条相同指令（问题原型）",
			Opts: Options{Bodies: 2, SubSteps: 3, Gravity: 2},
			Cmds: []Command{
				{Tick: 0, Body: 0, PushX: 1 << 16},
				{Tick: 0, Body: 0, PushX: 1 << 16},
			},
		},
		{
			Name: "B 多组、组间坐标不相交",
			Opts: Options{Bodies: 3, SubSteps: 2, Gravity: 0},
			Cmds: []Command{
				{Tick: 0, Body: 0, PushX: 1 << 15},
				{Tick: 0, Body: 0, PushX: 1 << 15},
				{Tick: 0, Body: 0, PushX: 1 << 15},
				{Tick: 0, Body: 1, PushY: -1 << 16},
				{Tick: 0, Body: 1, PushY: -1 << 16},
				// 帧号不同，不是重复，不得并入。
				{Tick: 1, Body: 1, PushY: -1 << 16},
			},
		},
		{
			Name: "C 零冲量重复",
			Opts: Options{Bodies: 3, SubSteps: 4, Gravity: 4},
			Cmds: []Command{
				{Tick: 0, Body: 1},
				{Tick: 0, Body: 1},
				{Tick: 0, Body: 1},
			},
		},
	}
}

// FormatTable 把审计结果排成三栏表：
//
//	第二栏「去重前哈希」= 原序逐条执行；另附归并/丢弃两种处理后的哈希供对照。
//	第三栏「丢失冲量」= 若按只留一条丢弃，相对原序少施加的 (m-1)d 之和。
//	第四栏「判定」= 归并是否与原序逐项相同，以及丢弃是否有损。
func FormatTable(results []ScenarioResult) string {
	out := "| 场景 | 去重前哈希(原序) | 归并后哈希 | 丢弃后哈希 | 丢失冲量 X,Y | 判定 |\n"
	out += "|---|---|---|---|---|---|\n"
	for _, r := range results {
		out += fmt.Sprintf("| %s | %016x | %016x | %016x | %d,%d | %s |\n",
			r.Name, r.HashOrig, r.HashMerged, r.HashDropped, r.LostX, r.LostY, r.Verdict)
	}
	return out
}

func snapshotEqual(a, b []int32) bool {
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

// hasConflict 报告是否有一对指令的目标与字段交集非空。
func hasConflict(cmds []Command) bool {
	for i := 0; i < len(cmds); i++ {
		for j := i + 1; j < len(cmds); j++ {
			if !Independent(cmds[i], cmds[j]) {
				return true
			}
		}
	}
	return false
}
