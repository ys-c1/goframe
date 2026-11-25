// Package verify 汇总子步数无关性的对拍、证明数据与回放记录结构，
// 供 go test 与 cmd/genartifacts 共同使用，避免验证逻辑和落盘逻辑各写一份。
package verify

import (
	"fmt"

	"goframe/api"
	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/sim"
	"goframe/replay"
)

// RequiredLevels 是任务明确要求覆盖的三档。
var RequiredLevels = []int{1, 2, 4}

// BoundaryLevel 是额外跑一次的边界档位。
const BoundaryLevel = 8

// AllLevels 返回三档加边界档，顺序固定。
func AllLevels() []int { return []int{1, 2, 4, BoundaryLevel} }

// CanonicalFrames 生成与 cmd/goframe run 默认演示完全一致的指令流：
// 第 i 帧给 body i%bodies 施加 PushX=1<<14。
func CanonicalFrames(frames, bodies int) []replay.Frame {
	fs := make([]replay.Frame, frames)
	for i := range fs {
		fs[i] = replay.Frame{Commands: []api.Command{{
			Tick:  i,
			Body:  i % bodies,
			PushX: 1 << 14,
		}}}
	}
	return fs
}

// RichFrames 用固定的简易 LCG 生成更“脏”的确定性指令流：
// 一帧内可能有多条指令，PushX/PushY 可正可负、且不按 Q16 低位对齐，
// 用来证明宽精度中点内核在 2 的幂子步档下对任意原始冲量都逐位无关。
func RichFrames(seed uint32, frames, bodies int) []replay.Frame {
	st := seed
	next := func() uint32 { // Numerical Recipes LCG，确定性
		st = st*1664525 + 1013904223
		return st
	}
	fs := make([]replay.Frame, frames)
	for f := range fs {
		ncmd := 1 + int(next()%3)
		cmds := make([]api.Command, 0, ncmd)
		for c := 0; c < ncmd; c++ {
			body := int(next() % uint32(bodies))
			sx := int32(next()%2000001) - 1000000
			sy := int32(next()%400001) - 200000
			cmds = append(cmds, api.Command{
				Tick:  f,
				Body:  body,
				PushX: int(sx),
				PushY: int(sy),
			})
		}
		fs[f] = replay.Frame{Commands: cmds}
	}
	return fs
}

// RunResult 是一次完整运行的逐帧指纹与原始状态。
type RunResult struct {
	Hashes []uint64
	Raw    [][]int32
}

// Run 用指定子步数跑同一段帧指令，收集每一帧末的 Hash 与 Snapshot。
func Run(bodies, sub, gravity int, frames []replay.Frame) (*RunResult, error) {
	opt := api.Options{Bodies: bodies, SubSteps: sub, Gravity: gravity}
	w, err := api.New(opt)
	if err != nil {
		return nil, err
	}
	rr := &RunResult{Hashes: make([]uint64, 0, len(frames)), Raw: make([][]int32, 0, len(frames))}
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			return nil, err
		}
		rr.Hashes = append(rr.Hashes, w.Hash())
		rr.Raw = append(rr.Raw, append([]int32(nil), w.Snapshot()...))
	}
	return rr, nil
}

// FrameDelta 是两档子步数在同一帧末的逐项比对。
type FrameDelta struct {
	Tick      int    `json:"tick"`
	HashA     string `json:"hash_a"`
	HashB     string `json:"hash_b"`
	HashEqual bool   `json:"hash_equal"`
	HashXOR   string `json:"hash_xor"`
	HashDelta uint64 `json:"hash_delta"` // 精确相等时 hA-hB 必为 0
	RawEqual  bool   `json:"raw_equal"`
}

// PairReport 是“两档子步数逐帧比对”的独立报告。
type PairReport struct {
	SubStepsA      int          `json:"substeps_a"`
	SubStepsB      int          `json:"substeps_b"`
	Frames         []FrameDelta `json:"frames"`
	AllHashEqual   bool         `json:"all_hash_equal"`
	AllDeltaZero   bool         `json:"all_delta_zero"`
	AllXORZero     bool         `json:"all_xor_zero"`
	AllRawEqual    bool         `json:"all_raw_equal"`
	DivergentTicks []int        `json:"divergent_ticks"`
}

// ComparePair 对两档结果逐帧比对。
func ComparePair(aSub, bSub int, a, b *RunResult) *PairReport {
	n := len(a.Hashes)
	if len(b.Hashes) < n {
		n = len(b.Hashes)
	}
	rep := &PairReport{SubStepsA: aSub, SubStepsB: bSub, AllHashEqual: true, AllDeltaZero: true, AllXORZero: true, AllRawEqual: true}
	for i := 0; i < n; i++ {
		ha, hb := a.Hashes[i], b.Hashes[i]
		fd := FrameDelta{
			Tick:      i,
			HashA:     fmt.Sprintf("%016x", ha),
			HashB:     fmt.Sprintf("%016x", hb),
			HashEqual: ha == hb,
			HashXOR:   fmt.Sprintf("0x%016x", ha^hb),
			HashDelta: ha - hb,
			RawEqual:  equalI32(a.Raw[i], b.Raw[i]),
		}
		if ha^hb != 0 {
			fd.HashXOR = fmt.Sprintf("0x%016x", ha^hb)
		}
		rep.Frames = append(rep.Frames, fd)
		if !fd.HashEqual {
			rep.AllHashEqual = false
			rep.DivergentTicks = append(rep.DivergentTicks, i)
		}
		if fd.HashDelta != 0 {
			rep.AllDeltaZero = false
		}
		if (ha ^ hb) != 0 {
			rep.AllXORZero = false
		}
		if !fd.RawEqual {
			rep.AllRawEqual = false
		}
	}
	return rep
}

// MatrixReport 覆盖 1/2/4 三档与 8 边界档的完整无关性矩阵。
type MatrixReport struct {
	Bodies        int         `json:"bodies"`
	Gravity       int         `json:"gravity"`
	FrameCount    int         `json:"frame_count"`
	Levels        []int       `json:"levels"`
	HashesByLevel [][]string  `json:"hashes_by_level"`
	SameFrame124  bool        `json:"same_frame_hash_1_2_4"`
	Boundary8Same bool        `json:"boundary_8_same_as_1"`
	AllLevelsSame bool        `json:"all_levels_same_as_1"`
	Pair1v4       *PairReport `json:"pair_1_vs_4_framewise_delta_zero"`
	Pair1v2       *PairReport `json:"pair_1_vs_2"`
	Pair2v4       *PairReport `json:"pair_2_vs_4"`
	Pair1v8       *PairReport `json:"pair_1_vs_8_boundary"`
}

// BuildMatrix 跑全部档位并构造矩阵报告；first 档作为基准。
func BuildMatrix(bodies, gravity int, frames []replay.Frame) (*MatrixReport, map[int]*RunResult, error) {
	levels := AllLevels()
	runs := map[int]*RunResult{}
	hashStr := make([][]string, len(levels))
	for li, n := range levels {
		rr, err := Run(bodies, n, gravity, frames)
		if err != nil {
			return nil, nil, err
		}
		runs[n] = rr
		hs := make([]string, len(rr.Hashes))
		for i, h := range rr.Hashes {
			hs[i] = fmt.Sprintf("%016x", h)
		}
		hashStr[li] = hs
	}
	m := &MatrixReport{
		Bodies:        bodies,
		Gravity:       gravity,
		FrameCount:    len(frames),
		Levels:        levels,
		HashesByLevel: hashStr,
	}
	m.Pair1v2 = ComparePair(1, 2, runs[1], runs[2])
	m.Pair2v4 = ComparePair(2, 4, runs[2], runs[4])
	m.Pair1v4 = ComparePair(1, 4, runs[1], runs[4])
	m.Pair1v8 = ComparePair(1, 8, runs[1], runs[8])

	m.SameFrame124 = runs[1] != nil && pairHashesEqual(runs[1], runs[2]) && pairHashesEqual(runs[1], runs[4])
	m.Boundary8Same = pairHashesEqual(runs[1], runs[8])
	m.AllLevelsSame = m.SameFrame124 && m.Boundary8Same
	return m, runs, nil
}

func pairHashesEqual(a, b *RunResult) bool {
	if a == nil || b == nil || len(a.Hashes) != len(b.Hashes) {
		return false
	}
	for i := range a.Hashes {
		if a.Hashes[i] != b.Hashes[i] {
			return false
		}
	}
	return true
}

func equalI32(a, b []int32) bool {
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

// ---- 旧 Euler 相关性与 G*h^2 二阶项证明 ----

// EulerHashRun 用“修复前”的逐字 Euler 内核跑同一段帧指令，
// 路径与初始提交的 api.Step 完全一致（Push 后每子步调一次 IntegrateEuler）。
func EulerHashRun(bodies, sub, gravity int, frames []replay.Frame) ([]uint64, error) {
	st := sim.NewState(bodies, fixed.FromInt(gravity))
	hs := make([]uint64, 0, len(frames))
	tick := 0
	for _, f := range frames {
		for _, c := range f.Commands {
			if c.Body < 0 || c.Body >= bodies {
				return nil, fmt.Errorf("body %d out of range", c.Body)
			}
			st.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
		}
		for i := 0; i < sub; i++ {
			st.IntegrateEuler(sub)
		}
		hs = append(hs, hash.State(tick, st))
		tick++
	}
	return hs, nil
}

// EulerProof 是当前（修复前）积分格式子步数相关性的精确数值证明。
type EulerProof struct {
	Gravity int `json:"gravity_world_units"`

	// 单实体、静止、只跑一帧时，修复前逐字代码的 y 末态（Q16 原始值）。
	LiteralYRaw map[int]int32 `json:"literal_code_y_raw_one_frame_from_rest"`
	// 对应世界单位 y = G(n+1)/2。
	LiteralYWorld map[int]string `json:"literal_code_y_world"`
	// n=1 与 n=4 的实测差（原始单位与世界单位），= 3G/2。
	LiteralGap1v4Raw  int64  `json:"literal_gap_1_minus_4_raw"`
	LiteralGapFormula string `json:"literal_gap_formula"`

	// 补上正确漂移权重 h 后的半隐式 Euler：y = G(n+1)/(2n) = G/2(1+h)。
	NormalizedYWorld map[int]string `json:"h_normalized_semiimplicit_y_world"`
	// n=1 与 n=4 的差 = 3G/8 = 6*G*h4^2，即“重力 × 子步长二阶项”的 6 倍。
	NormalizedGap1v4World string `json:"h_normalized_gap_1_vs_4_world"`
	H4SquaredTermWorld    string `json:"g_times_h4_squared_world"`
	CoefficientOnH4Sq     int    `json:"integer_triangular_coefficient"`

	// 在规范 20 帧指令流上，旧内核 1 档与 4 档逐帧哈希是否确实不同。
	CanonicalOldHashesDiffer bool `json:"canonical_old_euler_hashes_1_vs_4_differ"`
	CanonicalOldEqualFrames  int  `json:"canonical_old_euler_equal_frame_count"`
	CanonicalFrameCount      int  `json:"canonical_frame_count"`
}

// BuildEulerProof 计算所有证明数值。
func BuildEulerProof(gravity int, frames []replay.Frame) (*EulerProof, error) {
	p := &EulerProof{
		Gravity:          gravity,
		LiteralYRaw:      map[int]int32{},
		LiteralYWorld:    map[int]string{},
		NormalizedYWorld: map[int]string{},
	}
	G := int64(gravity)

	// 1) 逐字旧代码：单实体静止一帧。
	for _, n := range AllLevels() {
		st := sim.NewState(1, fixed.FromInt(gravity))
		for k := 0; k < n; k++ {
			st.IntegrateEuler(n)
		}
		raw := st.Raw()
		p.LiteralYRaw[n] = raw[1]
		p.LiteralYWorld[n] = rawToWorld(raw[1])
	}
	p.LiteralGap1v4Raw = int64(p.LiteralYRaw[1]) - int64(p.LiteralYRaw[4])
	p.LiteralGapFormula = "y_n = G*(n+1)/2 ; y_1 - y_4 = -3G/2 (signed 1minus4); magnitude 3G/2"

	// 2) h 正确的半隐式 Euler 闭式：y = G(n+1)/(2n)（世界单位，有理数）。
	for _, n := range AllLevels() {
		num := G * int64(n+1)
		den := 2 * int64(n)
		p.NormalizedYWorld[n] = fmt.Sprintf("%d/%d", num, den)
	}
	// y1 - y4 = G - 5G/8 = 3G/8
	p.NormalizedGap1v4World = fmt.Sprintf("%d/8", 3*G)
	// G*h4^2 = G/16
	p.H4SquaredTermWorld = fmt.Sprintf("%d/16", G)
	p.CoefficientOnH4Sq = 6

	// 3) 规范指令流上的真实旧哈希。
	h1, err := EulerHashRun(8, 1, gravity, frames)
	if err != nil {
		return nil, err
	}
	h4, err := EulerHashRun(8, 4, gravity, frames)
	if err != nil {
		return nil, err
	}
	p.CanonicalFrameCount = len(frames)
	for i := range h1 {
		if h1[i] == h4[i] {
			p.CanonicalOldEqualFrames++
		}
	}
	p.CanonicalOldHashesDiffer = p.CanonicalOldEqualFrames == 0 && len(h1) > 0
	return p, nil
}

func rawToWorld(v int32) string {
	neg := v < 0
	x := int64(v)
	if neg {
		x = -x
	}
	integer := x >> 16
	frac := x & 0xffff
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%05d(raw=%d)", sign, integer, frac*100000/65536, v)
}

// ReplayArtifact 是“一份回放记录”的落盘结构。
type ReplayArtifact struct {
	Scheme          string     `json:"scheme"`
	Log             replay.Log `json:"log"`
	RecordedHashes  []string   `json:"recorded_hashes"`
	PlaybackHashes  []string   `json:"playback_hashes"`
	ReplayIdentical bool       `json:"replay_identical"`
}

// BuildReplayArtifact 用 replay.Record/Playback 产生真实记录并自检。
func BuildReplayArtifact(opt api.Options, frames []replay.Frame) (*ReplayArtifact, error) {
	log, hashes, err := replay.Record(opt, frames)
	if err != nil {
		return nil, err
	}
	again, err := replay.Playback(log)
	if err != nil {
		return nil, err
	}
	a := &ReplayArtifact{Scheme: "explicit-midpoint + Q(16+16) guard bits", Log: log, ReplayIdentical: len(hashes) == len(again)}
	for i, h := range hashes {
		a.RecordedHashes = append(a.RecordedHashes, fmt.Sprintf("%016x", h))
		a.PlaybackHashes = append(a.PlaybackHashes, fmt.Sprintf("%016x", again[i]))
		if h != again[i] {
			a.ReplayIdentical = false
		}
	}
	return a, nil
}
