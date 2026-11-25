// Package parallel 提供并发步进内核，用于证明并验证“并发与串行等价 ⇔ 写入集合互不相交”。
//
// 本包不改动 internal/sim 的串行内核，而是在同一套定点语义上独立建模：
// 一次帧步进被展开成若干原子操作 Op，每个操作写入一个字段位置 Loc，
// 并由某个实体的属主 worker 执行。串行执行器按固定顺序应用 Op；
// 并发执行器把 Op 按执行者分区指派给 worker，worker 间以阶段屏障同步。
// 两种并发实现：
//
//   - Interleave：确定性交错器，按轮询顺序逐操作交错执行，可稳定复现
//     共享写入导致的末态差异（真实 goroutine 下数据竞争不可复现）。
//   - Parallel：真实 goroutine + 阶段屏障，只接受写入集合互不相交的计划。
//
// 所有数值仍为 Q16.16 定点加法，操作的增量大小有统一上界 M，
// 因此末态差异（L1 原始单位距离）满足 D <= 2·M·S·L，
// S 为共享写入次数（按字段统计），L 为单位置最大写入次数。
package parallel

import "goframe/internal/fixed"

// Field 标识实体状态中的一个字段。共享写入按 (Field, 实体编号) 这一粒度统计，
// 即“按字段统计”而不是把实体的四个字段合并计数。
type Field int

const (
	VelX Field = iota
	VelY
	PosX
	PosY
	// OutboxX/OutboxY 是两阶段计划中每个实体私有的发件箱，按源实体编号存放，
	// 只在 staged 计划中使用，不参与末态导出与哈希。
	// 源实体的属主只写自己的发件箱；目标实体在屏障后跨 worker 读取，
	// 读不影响写入集合互不相交条件。
	OutboxX
	OutboxY
)

// numPhysical 是参与末态导出的物理字段数（发件箱不算）。
const numPhysical = 4

// String 返回字段名，用于报告。
func (f Field) String() string {
	switch f {
	case VelX:
		return "velX"
	case VelY:
		return "velY"
	case PosX:
		return "posX"
	case PosY:
		return "posY"
	case OutboxX:
		return "outboxX"
	case OutboxY:
		return "outboxY"
	default:
		return "unknown"
	}
}

// Loc 是一个可写入位置：某实体的某个字段。这是写入集合的最小元素。
type Loc struct {
	Body  int
	Field Field
}

// Op 是一次原子状态变更：执行者 ExecBody 的属主 worker 对 Loc 位置执行 state += Delta。
// 模型只含读-改-写形式的定点加法，与 internal/sim 的 Push/Integrate 语义一致。
//
// ExecBody 为 -1（LocalExec）时表示执行者就是写入位置所属实体（属主本地写）；
// 零值不可复用为标记，因为 0 是合法实体编号。
// 朴素计划的跨实体耦合把 ExecBody 设为源实体、Loc 设为目标实体的速度字段：
// 源 worker 写目标 worker 拥有的位置，构成共享写入。
//
// Delta 为原始 Q16.16 表示；当 Compute 非空时，Delta 在应用时按当前状态计算，
// 用于跨实体耦合（冲量大小取决于源实体当前速度）。
type Op struct {
	ExecBody int
	Loc      Loc
	Delta    fixed.Value
	Compute  func(s *State) fixed.Value
}

// LocalExec 是 ExecBody 的哨兵值，表示执行者即写入位置的属主。
const LocalExec = -1

// localOp 构造一个属主本地写操作。
func localOp(loc Loc, delta fixed.Value) Op {
	return Op{ExecBody: LocalExec, Loc: loc, Delta: delta}
}

// localCompute 构造一个属主本地写的 Compute 类操作。
func localCompute(loc Loc, fn func(s *State) fixed.Value) Op {
	return Op{ExecBody: LocalExec, Loc: loc, Compute: fn}
}

// execBody 返回实际执行者实体编号。
func (op Op) execBody() int {
	if op.ExecBody != LocalExec {
		return op.ExecBody
	}
	return op.Loc.Body
}

// delta 返回该操作实际写入的增量，用于统计最大增量上界。
func (op Op) delta(s *State) fixed.Value {
	if op.Compute != nil {
		return op.Compute(s)
	}
	return op.Delta
}

// FramePlan 是一帧的全部操作，按阶段组织；阶段之间存在全局屏障，
// 同一阶段内的操作才可能并发。
//
// 朴素（naive）计划只有一个阶段，跨实体耦合由源 worker 直接写目标实体的速度，
// 跨分区时产生共享写入。
//
// 两阶段（staged）计划拆成五个屏障阶段：
//
//	阶段 0：外部冲量，只写本实体速度（按实体分区互不相交）；
//	阶段 1：耦合读速度快照，只写源实体私有的发件箱；
//	阶段 2：每个实体读前驱发件箱，把耦合落到自己的速度上，
//	        跨 worker 的只有读，且本阶段必须先于积分阶段；
//	阶段 3：屏障后统一做重力与位置积分，全部为属主本地写；
//	阶段 4：屏障后各属主清空自己的发件箱。
//
// 每个阶段的写入集合按实体分区互不相交；跨 worker 的只有读。
type FramePlan struct {
	Phases [][]Op
}

// State 是并发实验使用的实体状态，物理字段布局与 internal/sim.State 相同。
// outboxX/outboxY 是 staged 计划的源私有发件箱，按源实体编号存放，
// 不参与 Raw 导出与状态哈希。
type State struct {
	velX, velY []fixed.Value
	posX, posY []fixed.Value
	outboxX    []fixed.Value
	outboxY    []fixed.Value
	gravity    fixed.Value
}

// NewState 创建实体数为 n 的状态。
func NewState(n int, gravity fixed.Value) *State {
	return &State{
		velX:    make([]fixed.Value, n),
		velY:    make([]fixed.Value, n),
		posX:    make([]fixed.Value, n),
		posY:    make([]fixed.Value, n),
		outboxX: make([]fixed.Value, n),
		outboxY: make([]fixed.Value, n),
		gravity: gravity,
	}
}

// Len 返回实体数。
func (s *State) Len() int { return len(s.velX) }

// Clone 返回逐字段拷贝。
func (s *State) Clone() *State {
	n := s.Len()
	c := &State{
		velX:    make([]fixed.Value, n),
		velY:    make([]fixed.Value, n),
		posX:    make([]fixed.Value, n),
		posY:    make([]fixed.Value, n),
		outboxX: make([]fixed.Value, n),
		outboxY: make([]fixed.Value, n),
		gravity: s.gravity,
	}
	copy(c.velX, s.velX)
	copy(c.velY, s.velY)
	copy(c.posX, s.posX)
	copy(c.posY, s.posY)
	copy(c.outboxX, s.outboxX)
	copy(c.outboxY, s.outboxY)
	return c
}

// apply 把一个 Op 落到状态上（读-改-写：当前值加 Delta）。
func (s *State) apply(op Op) {
	d := op.delta(s)
	switch op.Loc.Field {
	case VelX:
		s.velX[op.Loc.Body] = s.velX[op.Loc.Body].Add(d)
	case VelY:
		s.velY[op.Loc.Body] = s.velY[op.Loc.Body].Add(d)
	case PosX:
		s.posX[op.Loc.Body] = s.posX[op.Loc.Body].Add(d)
	case PosY:
		s.posY[op.Loc.Body] = s.posY[op.Loc.Body].Add(d)
	case OutboxX:
		s.outboxX[op.Loc.Body] = s.outboxX[op.Loc.Body].Add(d)
	case OutboxY:
		s.outboxY[op.Loc.Body] = s.outboxY[op.Loc.Body].Add(d)
	}
}

// get 读取一个位置的当前值。
func (s *State) get(l Loc) fixed.Value {
	switch l.Field {
	case VelX:
		return s.velX[l.Body]
	case VelY:
		return s.velY[l.Body]
	case PosX:
		return s.posX[l.Body]
	case PosY:
		return s.posY[l.Body]
	case OutboxX:
		return s.outboxX[l.Body]
	default:
		return s.outboxY[l.Body]
	}
}

// set 写入一个位置的当前值，用于分半轮调度的写半轮提交。
func (s *State) set(l Loc, v fixed.Value) {
	switch l.Field {
	case VelX:
		s.velX[l.Body] = v
	case VelY:
		s.velY[l.Body] = v
	case PosX:
		s.posX[l.Body] = v
	case PosY:
		s.posY[l.Body] = v
	case OutboxX:
		s.outboxX[l.Body] = v
	case OutboxY:
		s.outboxY[l.Body] = v
	}
}

// Raw 按与 internal/sim.State 相同的固定顺序导出全部物理字段，不含发件箱。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, numPhysical*s.Len())
	for i := range s.velX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}

// Push 是一帧内对实体施加的外部冲量。
type Push struct {
	Body int
	DX   fixed.Value
	DY   fixed.Value
}

// Coupling 是跨实体耦合冲量：Body 给环上的后一个实体 (Body+1)%n 施力，
// 大小为 Body 当前速度右移 Shift 位（定点缩放，保证增量有统一上界）。
// 这是共享写入的唯一来源：朴素计划中源 worker 直接写目标 worker 的速度字段。
type Coupling struct {
	Body  int
	Shift uint
}
