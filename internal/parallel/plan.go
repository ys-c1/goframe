package parallel

import "goframe/internal/fixed"

// FrameInput 是一帧的外部输入：外部冲量与本帧启用的跨实体耦合。
type FrameInput struct {
	Pushes   []Push
	Coupling []Coupling
	SubSteps int
}

// shiftRight 实现定点值算术右移 k 位（向零取整），用于耦合冲量的缩放，
// 保证耦合增量不超过速度本身，从而所有操作的增量存在统一上界 M。
func shiftRight(v fixed.Value, k uint) fixed.Value {
	raw := int64(v.Raw())
	if raw >= 0 {
		return fixed.FromRaw(int(raw >> k))
	}
	return fixed.FromRaw(int(-((-raw) >> k)))
}

// gravityPerSub 复刻 internal/sim.Integrate 的重力分摊：gravity / sub。
func gravityPerSub(s *State, sub int) fixed.Value {
	return s.gravity.Div(fixed.FromInt(sub))
}

// postPushVelocity 计算外部冲量全部生效后的速度快照。
// 帧语义约定：本帧所有耦合冲量都基于“冲量后、耦合前”的同一速度快照计算，
// 与应用顺序无关，因此串行与并发共享同一份增量定义。
func postPushVelocity(s *State, in FrameInput) (vx, vy []fixed.Value) {
	n := s.Len()
	vx = make([]fixed.Value, n)
	vy = make([]fixed.Value, n)
	copy(vx, s.velX)
	copy(vy, s.velY)
	for _, p := range in.Pushes {
		vx[p.Body] = vx[p.Body].Add(p.DX)
		vy[p.Body] = vy[p.Body].Add(p.DY)
	}
	return vx, vy
}

// BuildNaive 构建朴素计划：所有操作放进单一阶段，耦合由源 worker 直接写
// 后一个实体的速度。跨分区时源 worker 写入目标 worker 拥有的位置，
// 构成共享写入（ExecBody=源实体，Loc=目标实体字段）。
func BuildNaive(s *State, in FrameInput) FramePlan {
	n := s.Len()
	var ops []Op

	// 1. 外部冲量（属主本地写）。
	for _, p := range in.Pushes {
		ops = append(ops,
			localOp(Loc{p.Body, VelX}, p.DX),
			localOp(Loc{p.Body, VelY}, p.DY),
		)
	}

	// 2. 跨实体耦合：源 i 的 worker 执行非原子读-改-写，直接写目标 (i+1)%n 的速度。
	//    增量按“冲量后速度快照”预算（与 staged 计划同一语义），通过闭包捕获常量；
	//    竞争点不在增量依赖，而在读-改-写本身：双相调度的读半轮让属主更新与
	//    源 worker 耦合写捕获同一 base，写半轮互相覆盖，复现丢失更新。
	snapX, snapY := postPushVelocity(s, in)
	for _, c := range in.Coupling {
		c := c // 块级局部，避免 Go 1.21 循环变量共享导致闭包捕获末值
		src := c.Body
		target := (src + 1) % n
		dx := shiftRight(snapX[src], c.Shift)
		dy := shiftRight(snapY[src], c.Shift)
		ops = append(ops,
			Op{
				ExecBody: src,
				Loc:      Loc{target, VelX},
				Compute: func(st *State) fixed.Value {
					return dx
				},
			},
			Op{
				ExecBody: src,
				Loc:      Loc{target, VelY},
				Compute: func(st *State) fixed.Value {
					return dy
				},
			},
		)
	}

	// 3. 子步积分：重力、位置积分，顺序与 internal/sim.Integrate 相同。
	appendIntegrate(&ops, n, gravityPerSub(s, in.SubSteps), in.SubSteps)

	return FramePlan{Phases: [][]Op{ops}}
}

// appendIntegrate 追加子步积分操作（全部为属主本地写）。
func appendIntegrate(ops *[]Op, n int, g fixed.Value, substeps int) {
	for sub := 0; sub < substeps; sub++ {
		for i := 0; i < n; i++ {
			*ops = append(*ops, localOp(Loc{i, VelY}, g))
		}
		for i := 0; i < n; i++ {
			i := i // 块级局部，避免 Go 1.21 循环变量共享导致闭包捕获末值
			*ops = append(*ops,
				localCompute(Loc{i, PosX}, func(st *State) fixed.Value {
					return st.velX[i]
				}),
				localCompute(Loc{i, PosY}, func(st *State) fixed.Value {
					return st.velY[i]
				}),
			)
		}
	}
}

// BuildStaged 构建两阶段（五个屏障阶段）计划，串行语义与 BuildNaive 逐字节相同。
//
//	阶段 0：外部冲量（只写本实体速度）；
//	阶段 1：每个源实体把耦合冲量写入自己的私有发件箱（只写本实体字段）；
//	阶段 2：每个实体读前驱的发件箱落到自己的速度上（跨 worker 的只是读）；
//	阶段 3：屏障后统一做重力与位置积分；
//	阶段 4：屏障后各属主清空自己的发件箱。
//
// 每个阶段的写入位置均按实体分区互不相交。
func BuildStaged(s *State, in FrameInput) FramePlan {
	n := s.Len()

	// 阶段 0：外部冲量。
	var phase0 []Op
	for _, p := range in.Pushes {
		phase0 = append(phase0,
			localOp(Loc{p.Body, VelX}, p.DX),
			localOp(Loc{p.Body, VelY}, p.DY),
		)
	}

	// 阶段 1：源实体写自己的发件箱（属主本地写）。
	var phase1 []Op
	snapX, snapY := postPushVelocity(s, in)
	for _, c := range in.Coupling {
		phase1 = append(phase1,
			localOp(Loc{c.Body, OutboxX}, shiftRight(snapX[c.Body], c.Shift)),
			localOp(Loc{c.Body, OutboxY}, shiftRight(snapY[c.Body], c.Shift)),
		)
	}

	// 阶段 2：每个实体读前驱的发件箱，把耦合落到自己的速度上。
	// 跨 worker 的只是读；落盘必须先于积分，故独立成屏障阶段，
	// 保证任何 worker 进入积分前所有速度都已包含耦合。
	var phase2 []Op
	for i := 0; i < n; i++ {
		i := i // 块级局部，避免循环变量共享导致闭包捕获末值
		body := i
		pred := (i - 1 + n) % n
		phase2 = append(phase2,
			localCompute(Loc{body, VelX}, func(st *State) fixed.Value {
				return st.outboxX[pred]
			}),
			localCompute(Loc{body, VelY}, func(st *State) fixed.Value {
				return st.outboxY[pred]
			}),
		)
	}

	// 阶段 3：屏障后统一做重力与位置积分（属主本地写，顺序与串行一致）。
	var phase3 []Op
	appendIntegrate(&phase3, n, gravityPerSub(s, in.SubSteps), in.SubSteps)

	// 阶段 4：屏障之后由各属主清空自己的发件箱。
	// 清理必须独立成阶段：阶段 2 中其他 worker 仍可能跨 worker 读该发件箱，
	// 同阶段内读写并存会构成真实数据竞争；独立屏障阶段保证读全部完成后再写。
	var phase4 []Op
	for i := 0; i < n; i++ {
		body := i // i 仅用于切片索引，body 被闭包捕获，须为块级局部
		phase4 = append(phase4,
			localCompute(Loc{body, OutboxX}, func(st *State) fixed.Value {
				return fixed.Value(0).Sub(st.outboxX[body])
			}),
			localCompute(Loc{body, OutboxY}, func(st *State) fixed.Value {
				return fixed.Value(0).Sub(st.outboxY[body])
			}),
		)
	}

	return FramePlan{Phases: [][]Op{phase0, phase1, phase2, phase3, phase4}}
}
