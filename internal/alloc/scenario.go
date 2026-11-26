// Package alloc 是编号分配策略的对照实验：
// 同三组增删序列分别在"槽位复用"引擎与"单调编号"引擎上执行，
// 用同一套 FNV 混合规则取指纹，量化复用次数与哈希错位的关系。
package alloc

// OpKind 是实验序列中的操作类型。
type OpKind int

const (
	OpSpawn OpKind = iota
	OpDespawn
	OpPush
)

// Op 是一条带帧号与逻辑实体名的操作。实体名在同一序列内唯一，
// 两个引擎各自把名字映射到自己分配的编号/槽位。
type Op struct {
	Frame int
	Kind  OpKind
	Name  string
	PushX int32
	PushY int32
}

// Scenario 是一组对照：三个序列的存活者操作完全一致，
// 仅临时实体的增删时机不同。
type Scenario struct {
	Name      string
	Survivors []string // 三组序列共同存活到最后的实体名（有序）
	Sequences [][]Op
}

// pushSurvivors 在统一帧号对存活者施加完全相同的冲量，
// 保证三组序列终态的物理数值一致。
func pushSurvivors(frame int, names []string) []Op {
	ops := make([]Op, 0, len(names))
	for i, n := range names {
		ops = append(ops, Op{
			Frame: frame,
			Kind:  OpPush,
			Name:  n,
			PushX: int32((i + 1) << 16),
			PushY: int32(-(i + 1) << 14),
		})
	}
	return ops
}

// Scenarios 返回三组增删序列。每组序列总生成数相同、存活者相同、
// 存活者冲量相同，只有临时实体的销毁/穿插时机不同。
func Scenarios() []Scenario {
	return []Scenario{
		// 场景一：两个临时实体，销毁与存活者生成的穿插顺序不同。
		{
			Name:      "two-temp-interleave",
			Survivors: []string{"A", "B", "C"},
			Sequences: [][]Op{
				{
					{Frame: 1, Kind: OpSpawn, Name: "A"},
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 1, Kind: OpDespawn, Name: "T1"},
					{Frame: 1, Kind: OpDespawn, Name: "T2"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
				},
				{
					{Frame: 1, Kind: OpSpawn, Name: "A"},
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 1, Kind: OpDespawn, Name: "T1"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
					{Frame: 2, Kind: OpDespawn, Name: "T2"},
				},
				{
					{Frame: 1, Kind: OpSpawn, Name: "A"},
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
					{Frame: 2, Kind: OpDespawn, Name: "T1"},
					{Frame: 2, Kind: OpDespawn, Name: "T2"},
				},
			},
		},
		// 场景二：复用全部发生在存活者生成之前，可直接观测
		// "首个共同存活者的位移 = 复用次数差 × 槽位字数"。
		{
			Name:      "reuse-before-survivors",
			Survivors: []string{"A", "B", "C"},
			Sequences: [][]Op{
				{
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 1, Kind: OpDespawn, Name: "T1"},
					{Frame: 1, Kind: OpDespawn, Name: "T2"},
					{Frame: 2, Kind: OpSpawn, Name: "A"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
				},
				{
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 1, Kind: OpDespawn, Name: "T1"},
					{Frame: 2, Kind: OpSpawn, Name: "A"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
					{Frame: 3, Kind: OpDespawn, Name: "T2"},
				},
				{
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 2, Kind: OpSpawn, Name: "A"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
					{Frame: 3, Kind: OpDespawn, Name: "T1"},
					{Frame: 3, Kind: OpDespawn, Name: "T2"},
				},
			},
		},
		// 场景三：三个临时实体，复用次数 3/1/0，覆盖更长的空洞链。
		{
			Name:      "three-temp",
			Survivors: []string{"A", "B", "C"},
			Sequences: [][]Op{
				{
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 1, Kind: OpSpawn, Name: "T3"},
					{Frame: 1, Kind: OpDespawn, Name: "T1"},
					{Frame: 1, Kind: OpDespawn, Name: "T2"},
					{Frame: 1, Kind: OpDespawn, Name: "T3"},
					{Frame: 2, Kind: OpSpawn, Name: "A"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
				},
				{
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 1, Kind: OpSpawn, Name: "T3"},
					{Frame: 1, Kind: OpDespawn, Name: "T1"},
					{Frame: 2, Kind: OpSpawn, Name: "A"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
					{Frame: 3, Kind: OpDespawn, Name: "T2"},
					{Frame: 3, Kind: OpDespawn, Name: "T3"},
				},
				{
					{Frame: 1, Kind: OpSpawn, Name: "T1"},
					{Frame: 1, Kind: OpSpawn, Name: "T2"},
					{Frame: 1, Kind: OpSpawn, Name: "T3"},
					{Frame: 2, Kind: OpSpawn, Name: "A"},
					{Frame: 2, Kind: OpSpawn, Name: "B"},
					{Frame: 2, Kind: OpSpawn, Name: "C"},
					{Frame: 3, Kind: OpDespawn, Name: "T1"},
					{Frame: 3, Kind: OpDespawn, Name: "T2"},
					{Frame: 3, Kind: OpDespawn, Name: "T3"},
				},
			},
		},
	}
}

// maxFrame 返回场景内用到的最大帧号；存活者冲量统一在 maxFrame+1 帧施加。
func (s Scenario) maxFrame() int {
	m := 0
	for _, op := range s.Sequences[0] {
		if op.Frame > m {
			m = op.Frame
		}
	}
	for _, seq := range s.Sequences[1:] {
		for _, op := range seq {
			if op.Frame > m {
				m = op.Frame
			}
		}
	}
	return m
}
