package parallel

// Owner 返回实体 body 在 workers 个 worker 的连续环分区下的属主编号。
// 实体按编号连续切块：worker w 拥有 [w*n/W, (w+1)*n/W) 的实体，
// 余数并入最后一个 worker。分区只按实体划分，实体的全部字段归同一属主。
func Owner(body, workers, n int) int {
	if workers <= 1 {
		return 0
	}
	w := body * workers / n
	if w >= workers {
		w = workers - 1
	}
	return w
}

// FieldStat 是单个字段的共享写入统计。统计单位是字段位置 Loc=(实体,字段)，
// 即“按字段统计”，不把同一实体的四个字段合并。
type FieldStat struct {
	Field        Field
	Writes       int // 该字段全部位置的总写入次数
	SharedWrites int // 非属主 worker 写入该字段位置的次数
	SharedBodies int // 该字段上发生共享写入的不同实体数
}

// WriteReport 是整段运行（所有帧、所有阶段）的写入集合分析结果。
type WriteReport struct {
	Workers      int
	PerField     [numPhysical]FieldStat // 仅物理字段；发件箱是实现细节，单列
	OutboxShared int                    // 发件箱上的共享写入次数（修复版应为 0）
	TotalWrites  int
	SharedWrites int  // 全字段（含发件箱）共享写入总次数 S
	SharedBodies int  // 存在共享写入的不同实体数（仅物理字段）
	Disjoint     bool // 全部阶段写入集合是否互不相交
}

// locKey 把字段位置压成可做 map 键的整数。
type locKey struct {
	body  int
	field Field
}

type locCounter struct {
	writes      int          // 总写入次数
	ownerWrites int          // 位置属主的写入次数
	otherWrites int          // 非属主 worker 的写入次数
	execWorkers map[int]bool // 实际执行过写入的 worker 集合
}

// Analyze 按连续环分区分析一组帧计划的写入集合。
//
// 关键区分：操作的“执行者”是 ExecBody 的属主 worker，写入位置的“属主”
// 是 Loc.Body 的属主 worker。朴素耦合中二者不同，正是共享写入的来源。
//
// 共享写入统计口径（按字段，逐位置计数）：
//
//  1. 最小单位是字段位置 Loc=(实体 Body, 字段 Field)，不是整个实体；
//  2. 每个操作按执行者 ExecBody 的属主 Owner(ExecBody) 归属 worker；
//  3. 对每个位置 L 记录总写入次数 K(L)、属主写入 K_own(L)、
//     非属主写入 K_other(L)、执行写入的 worker 集合 W(L)；
//  4. 当 |W(L)| >= 2 时该位置发生共享写入，共享写入次数
//     S(L) = K_other(L)，即所有非属主 worker 的写入次数；
//  5. 总共享写入次数 S = Σ_L S(L)，按字段分别汇总，
//     并同时报告涉及共享写入的实体数。
func Analyze(plans []FramePlan, workers, n int) WriteReport {
	locs := make(map[locKey]*locCounter)

	for _, plan := range plans {
		for _, phase := range plan.Phases {
			for _, op := range phase {
				k := locKey{op.Loc.Body, op.Loc.Field}
				c := locs[k]
				if c == nil {
					c = &locCounter{execWorkers: make(map[int]bool)}
					locs[k] = c
				}
				execW := Owner(op.execBody(), workers, n)
				locOwnerW := Owner(op.Loc.Body, workers, n)
				c.writes++
				c.execWorkers[execW] = true
				if execW == locOwnerW {
					c.ownerWrites++
				} else {
					c.otherWrites++
				}
			}
		}
	}

	r := WriteReport{Workers: workers, Disjoint: true}
	for _, c := range locs {
		r.TotalWrites += c.writes
		if len(c.execWorkers) >= 2 {
			r.Disjoint = false
			r.SharedWrites += c.otherWrites
		}
	}
	for k, c := range locs {
		if len(c.execWorkers) < 2 {
			continue
		}
		switch k.field {
		case OutboxX, OutboxY:
			r.OutboxShared += c.otherWrites
		default:
			r.PerField[k.field].SharedWrites += c.otherWrites
		}
	}
	// 每个物理字段的总写入次数（含未发生共享的位置）与涉及实体数。
	for f := 0; f < numPhysical; f++ {
		bodies := make(map[int]bool)
		for k, c := range locs {
			if k.field != Field(f) {
				continue
			}
			r.PerField[f].Field = Field(f)
			r.PerField[f].Writes += c.writes
			if len(c.execWorkers) >= 2 {
				bodies[k.body] = true
			}
		}
		r.PerField[f].SharedBodies = len(bodies)
	}
	allShared := make(map[int]bool)
	for k, c := range locs {
		if len(c.execWorkers) >= 2 && k.field < numPhysical {
			allShared[k.body] = true
		}
	}
	r.SharedBodies = len(allShared)
	return r
}
