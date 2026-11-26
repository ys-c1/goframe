// Package drift 用仓库内确定性内核复现「旧序快照回放漂移」，
// 计算漂移量关于帧数 f 与字段数 N 的精确公式残差，并汇总 2×2 归因。
package drift

import (
	"encoding/binary"
	"fmt"

	"goframe/api"
	"goframe/internal/snapfmt"
)

// Spec 定义一份三案快照的产生方式。
// 第 t 帧（t 从 0 起）下发一条指令：Body=t%Bodies, PushX=PushRaw。
type Spec struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Bodies     int    `json:"bodies"`
	Gravity    int    `json:"gravity"`
	SubSteps   int    `json:"-"`
	Checkpoint int    `json:"checkpoint"`
	Extra      int    `json:"extra"`
	PushRaw    int    `json:"push_raw"`
}

// DefaultSpecs 是三份快照的参数：检查点分别在第 36/48/60 帧（均为几十帧）。
func DefaultSpecs() []Spec {
	return []Spec{
		{ID: "case1", Label: "N=16 / 第36帧快照", Bodies: 4, Gravity: 1, SubSteps: 1, Checkpoint: 36, Extra: 24, PushRaw: 1 << 16},
		{ID: "case2", Label: "N=32 / 第48帧快照", Bodies: 8, Gravity: 1, SubSteps: 1, Checkpoint: 48, Extra: 24, PushRaw: 1 << 16},
		{ID: "case3", Label: "N=64 / 第60帧快照", Bodies: 16, Gravity: 1, SubSteps: 1, Checkpoint: 60, Extra: 24, PushRaw: 1 << 16},
	}
}

// Options 返回对应的 api.Options。
func (s Spec) Options() api.Options {
	return api.Options{Bodies: s.Bodies, SubSteps: s.SubSteps, Gravity: s.Gravity}
}

// Commands 构造 total 帧的确定性指令序列。
func (s Spec) Commands(total int) [][]api.Command {
	cmds := make([][]api.Command, total)
	for t := 0; t < total; t++ {
		if s.PushRaw == 0 {
			continue
		}
		cmds[t] = []api.Command{{Tick: t, Body: t % s.Bodies, PushX: s.PushRaw}}
	}
	return cmds
}

// FrameRow 是检查点之后第 f 帧（f=0 为载入当下）的逐帧比对数据。
type FrameRow struct {
	F                     int     `json:"f"`
	Tick                  int     `json:"tick"`
	DirectHash            string  `json:"direct_hash"`
	NaiveHash             string  `json:"naive_hash"`
	NormalizedHash        string  `json:"normalized_hash"`
	CanonicalFileHash     string  `json:"canonical_file_hash"`
	NaiveHashEqual        bool    `json:"naive_hash_equal"`
	NormalizedHashEqual   bool    `json:"normalized_hash_equal"`
	Dx0Raw                int64   `json:"dx0_raw"`
	Dy0Raw                int64   `json:"dy0_raw"`
	PosMaxAbsRaw          int64   `json:"pos_max_abs_raw"`
	PosMaxAbsUnits        float64 `json:"pos_max_abs_units"`
	PredDx0Raw            int64   `json:"pred_dx0_raw"`
	PredDy0Raw            int64   `json:"pred_dy0_raw"`
	FormulaMaxAbsResidual int64   `json:"formula_max_abs_residual"`
	FormulaExact          bool    `json:"formula_exact"`
}

// FactorialRow 是字段序 × 字节序 2×2 归因中的一行。
type FactorialRow struct {
	Mode                 string `json:"mode"`
	Endian               string `json:"endian"`
	Order                string `json:"order"`
	RecoversExactState   bool   `json:"recovers_exact_state"`
	FirstDiffF           *int   `json:"first_diff_f"`
	FirstDiffTick        *int   `json:"first_diff_tick"`
	DevAtLoadRaw         int64  `json:"dev_at_load_raw"`
	DevAtEndRaw          int64  `json:"dev_at_end_raw"`
	FinalHash            string `json:"final_hash"`
	AllHashesEqualDirect bool   `json:"all_hashes_equal_direct"`
}

// CaseResult 汇总一份快照的全部实验结果与序列化字节。
type CaseResult struct {
	Spec    Spec        `json:"spec"`
	SpecN   int         `json:"field_count_N"`
	Options api.Options `json:"options"`

	CheckpointHashDirect     string `json:"checkpoint_hash_direct"`
	HeaderStateHash          string `json:"header_state_hash"`
	NaiveCheckpointHash      string `json:"naive_checkpoint_hash"`
	NormalizedCheckpointHash string `json:"normalized_checkpoint_hash"`
	DirectFinalHash          string `json:"direct_final_hash"`
	NaiveFinalHash           string `json:"naive_final_hash"`
	NormalizedFinalHash      string `json:"normalized_final_hash"`

	DriftStartTickInclCheckpoint *int `json:"drift_start_tick_including_checkpoint"`
	FirstSteppedDivergentTick    *int `json:"first_stepped_divergent_tick"`
	NormalizedDriftStartTick     *int `json:"normalized_drift_start_tick"`

	DeviationAtDriftStartRaw  int64   `json:"deviation_at_drift_start_raw"`
	DeviationAtDriftStartUnit float64 `json:"deviation_at_drift_start_units"`
	DeviationAtFirstStepRaw   int64   `json:"deviation_at_first_step_raw"`
	DeviationAtEndRaw         int64   `json:"deviation_at_end_raw"`
	DeviationAtEndUnits       float64 `json:"deviation_at_end_units"`

	FirstWholeUnitDiffF    *int `json:"first_whole_unit_diff_f"`
	FirstWholeUnitDiffTick *int `json:"first_whole_unit_diff_tick"`

	FormulaExactAllFrames bool  `json:"formula_exact_all_frames"`
	FormulaMaxAbsResidual int64 `json:"formula_max_abs_residual"`
	FormulaCheckedFrames  int   `json:"formula_checked_frames"`

	LegacyFile    string `json:"legacy_file,omitempty"`
	CanonicalFile string `json:"canonical_file,omitempty"`
	LegacySHA256  string `json:"legacy_sha256,omitempty"`
	CanonicalSHA  string `json:"canonical_sha256,omitempty"`
	LegacySize    int    `json:"legacy_size_bytes"`
	CanonicalSize int    `json:"canonical_size_bytes"`

	Frames    []FrameRow     `json:"frames"`
	Factorial []FactorialRow `json:"factorial"`

	LegacyBytes    []byte `json:"-"`
	CanonicalBytes []byte `json:"-"`
}

func hx(h uint64) string { return fmt.Sprintf("%016x", h) }

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// errVector 计算 replay 与 direct 两个 raw 状态的有符号差。
func errVector(replay, direct []int32) []int64 {
	e := make([]int64, len(direct))
	for k := range direct {
		e[k] = int64(replay[k]) - int64(direct[k])
	}
	return e
}

// posMaxAbs 返回位置字段（每实体前两个）的最大绝对偏差。
func posMaxAbs(errs []int64, B int) int64 {
	var m int64
	for b := 0; b < B; b++ {
		m = max64(m, abs64(errs[4*b+0]))
		m = max64(m, abs64(errs[4*b+1]))
	}
	return m
}

// predictedError 依据精确线性公式预测第 f 帧的误差向量：
//
//	Δx_b(f,N)=e_{4b} + f·S·e_{4b+2}
//	Δy_b(f,N)=e_{4b+1} + f·S·e_{4b+3}
//	速度误差恒定。
func predictedError(loadErr []int64, B, S, f int) []int64 {
	q := int64(f * S)
	p := make([]int64, 4*B)
	for b := 0; b < B; b++ {
		p[4*b+0] = loadErr[4*b+0] + q*loadErr[4*b+2]
		p[4*b+1] = loadErr[4*b+1] + q*loadErr[4*b+3]
		p[4*b+2] = loadErr[4*b+2]
		p[4*b+3] = loadErr[4*b+3]
	}
	return p
}

// runMode 用给定解释出的 raw 恢复世界并续跑，返回每帧 hash/raw。
func runMode(spec Spec, interp []int32, cmds [][]api.Command) (hashes []uint64, raws [][]int32, err error) {
	w, err := api.Restore(spec.Options(), spec.Checkpoint, interp)
	if err != nil {
		return nil, nil, err
	}
	hashes = make([]uint64, spec.Extra+1)
	raws = make([][]int32, spec.Extra+1)
	hashes[0] = w.Hash()
	raws[0] = append([]int32(nil), w.Snapshot()...)
	for f := 1; f <= spec.Extra; f++ {
		if err := w.Step(cmds[spec.Checkpoint+f-1]); err != nil {
			return nil, nil, err
		}
		hashes[f] = w.Hash()
		raws[f] = append([]int32(nil), w.Snapshot()...)
	}
	return hashes, raws, nil
}

// directRun 跑直跑，返回每个 tick 的 hash 与 raw。
func directRun(spec Spec, cmds [][]api.Command) ([]uint64, [][]int32, error) {
	total := spec.Checkpoint + spec.Extra
	w, err := api.New(spec.Options())
	if err != nil {
		return nil, nil, err
	}
	hh := make([]uint64, total+1)
	rr := make([][]int32, total+1)
	hh[0] = w.Hash()
	rr[0] = append([]int32(nil), w.Snapshot()...)
	for t := 0; t < total; t++ {
		if err := w.Step(cmds[t]); err != nil {
			return nil, nil, err
		}
		hh[t+1] = w.Hash()
		rr[t+1] = append([]int32(nil), w.Snapshot()...)
	}
	return hh, rr, nil
}

// RunCase 完整跑一份三案快照实验。
func RunCase(spec Spec) (*CaseResult, error) {
	N := 4 * spec.Bodies
	total := spec.Checkpoint + spec.Extra
	cmds := spec.Commands(total)

	directHash, directRaw, err := directRun(spec, cmds)
	if err != nil {
		return nil, err
	}
	c := directRaw[spec.Checkpoint]
	hC := directHash[spec.Checkpoint]

	snap := snapfmt.Snap{
		Bodies: spec.Bodies, SubSteps: spec.SubSteps, Gravity: spec.Gravity,
		Tick: spec.Checkpoint, Hash: hC, Raw: c,
	}
	legacy := snapfmt.EncodeLegacyV1(snap)
	canonical := snapfmt.EncodeCanonicalV2(snap)

	naiveSnap, err := snapfmt.DecodeLegacyV1Naive(legacy)
	if err != nil {
		return nil, err
	}
	normSnap, err := snapfmt.DecodeLegacyV1Normalized(legacy)
	if err != nil {
		return nil, err
	}
	v2Snap, err := snapfmt.DecodeCanonicalV2(canonical)
	if err != nil {
		return nil, err
	}

	naiveH, naiveR, err := runMode(spec, naiveSnap.Raw, cmds)
	if err != nil {
		return nil, err
	}
	normH, _, err := runMode(spec, normSnap.Raw, cmds)
	if err != nil {
		return nil, err
	}
	v2H, _, err := runMode(spec, v2Snap.Raw, cmds)
	if err != nil {
		return nil, err
	}

	loadErr := errVector(naiveR[0], c)

	res := &CaseResult{
		Spec: spec, SpecN: N, Options: spec.Options(),
		CheckpointHashDirect:     hx(hC),
		HeaderStateHash:          hx(hC),
		NaiveCheckpointHash:      hx(naiveH[0]),
		NormalizedCheckpointHash: hx(normH[0]),
		DirectFinalHash:          hx(directHash[total]),
		NaiveFinalHash:           hx(naiveH[spec.Extra]),
		NormalizedFinalHash:      hx(normH[spec.Extra]),
		LegacyBytes:              legacy,
		CanonicalBytes:           canonical,
		LegacySize:               len(legacy),
		CanonicalSize:            len(canonical),
	}

	var globalMaxResidual int64
	allExact := true
	for f := 0; f <= spec.Extra; f++ {
		tick := spec.Checkpoint + f
		errs := errVector(naiveR[f], directRaw[tick])
		pred := predictedError(loadErr, spec.Bodies, spec.SubSteps, f)
		var residMax int64
		for k := range errs {
			residMax = max64(residMax, abs64(errs[k]-pred[k]))
		}
		globalMaxResidual = max64(globalMaxResidual, residMax)
		if residMax != 0 {
			allExact = false
		}
		pMax := posMaxAbs(errs, spec.Bodies)

		res.Frames = append(res.Frames, FrameRow{
			F:                     f,
			Tick:                  tick,
			DirectHash:            hx(directHash[tick]),
			NaiveHash:             hx(naiveH[f]),
			NormalizedHash:        hx(normH[f]),
			CanonicalFileHash:     hx(v2H[f]),
			NaiveHashEqual:        naiveH[f] == directHash[tick],
			NormalizedHashEqual:   normH[f] == directHash[tick] && v2H[f] == directHash[tick],
			Dx0Raw:                errs[0],
			Dy0Raw:                errs[1],
			PosMaxAbsRaw:          pMax,
			PosMaxAbsUnits:        float64(pMax) / 65536.0,
			PredDx0Raw:            pred[0],
			PredDy0Raw:            pred[1],
			FormulaMaxAbsResidual: residMax,
			FormulaExact:          residMax == 0,
		})
	}
	res.FormulaExactAllFrames = allExact
	res.FormulaMaxAbsResidual = globalMaxResidual
	res.FormulaCheckedFrames = spec.Extra + 1

	// 漂移起始帧（哈希层面，含载入检查点）。
	for f := 0; f <= spec.Extra; f++ {
		if !res.Frames[f].NaiveHashEqual {
			t := spec.Checkpoint + f
			res.DriftStartTickInclCheckpoint = &t
			break
		}
	}
	if res.DriftStartTickInclCheckpoint != nil {
		t := spec.Checkpoint + 1
		res.FirstSteppedDivergentTick = &t
	}
	for f := 0; f <= spec.Extra; f++ {
		if !res.Frames[f].NormalizedHashEqual {
			t := spec.Checkpoint + f
			res.NormalizedDriftStartTick = &t
			break
		}
	}

	res.DeviationAtDriftStartRaw = res.Frames[0].PosMaxAbsRaw
	res.DeviationAtDriftStartUnit = res.Frames[0].PosMaxAbsUnits
	res.DeviationAtFirstStepRaw = res.Frames[1].PosMaxAbsRaw
	res.DeviationAtEndRaw = res.Frames[spec.Extra].PosMaxAbsRaw
	res.DeviationAtEndUnits = res.Frames[spec.Extra].PosMaxAbsUnits

	// 整数单位位置首次不同的帧（任一只位置字段 raw>>16 不同）。
	for f := 0; f <= spec.Extra; f++ {
		tick := spec.Checkpoint + f
		diff := false
		for b := 0; b < spec.Bodies; b++ {
			if naiveR[f][4*b+0]>>16 != directRaw[tick][4*b+0]>>16 ||
				naiveR[f][4*b+1]>>16 != directRaw[tick][4*b+1]>>16 {
				diff = true
				break
			}
		}
		if diff {
			fv, tv := f, tick
			res.FirstWholeUnitDiffF = &fv
			res.FirstWholeUnitDiffTick = &tv
			break
		}
	}

	res.Factorial, err = runFactorial(spec, legacy, directHash, directRaw, cmds)
	if err != nil {
		return nil, err
	}
	return res, nil
}

type modeDef struct {
	key      string
	endian   binary.ByteOrder
	endLbl   string
	reverse  bool
	orderLbl string
}

func factorialModes() []modeDef {
	return []modeDef{
		{"naive_LeIdentity_规范化前", binary.LittleEndian, "小端(错)", false, "不反转(错)"},
		{"order_only_仅字段序错", binary.BigEndian, "大端(对)", false, "不反转(错)"},
		{"endian_only_仅字节序错", binary.LittleEndian, "小端(错)", true, "反转(对)"},
		{"normalized_BeReverse_规范化后", binary.BigEndian, "大端(对)", true, "反转(对)"},
	}
}

func runFactorial(spec Spec, legacy []byte, directHash []uint64, directRaw [][]int32, cmds [][]api.Command) ([]FactorialRow, error) {
	total := spec.Checkpoint + spec.Extra
	out := make([]FactorialRow, 0, 4)
	for _, md := range factorialModes() {
		interp := snapfmt.InterpretState(snapfmt.LegacyBlock(legacy), md.endian, md.reverse)
		c := directRaw[spec.Checkpoint]
		recExact := true
		for k := range c {
			if interp[k] != c[k] {
				recExact = false
				break
			}
		}
		hh, rr, err := runMode(spec, interp, cmds)
		if err != nil {
			return nil, err
		}
		row := FactorialRow{
			Mode:                 md.key,
			Endian:               md.endLbl,
			Order:                md.orderLbl,
			RecoversExactState:   recExact,
			DevAtLoadRaw:         posMaxAbs(errVector(rr[0], directRaw[spec.Checkpoint]), spec.Bodies),
			DevAtEndRaw:          posMaxAbs(errVector(rr[spec.Extra], directRaw[total]), spec.Bodies),
			FinalHash:            hx(hh[spec.Extra]),
			AllHashesEqualDirect: true,
		}
		var firstF *int
		for f := 0; f <= spec.Extra; f++ {
			tick := spec.Checkpoint + f
			if hh[f] != directHash[tick] {
				row.AllHashesEqualDirect = false
				if firstF == nil {
					v := f
					firstF = &v
				}
			}
		}
		if firstF != nil {
			ff := *firstF
			tt := spec.Checkpoint + ff
			row.FirstDiffF = &ff
			row.FirstDiffTick = &tt
		}
		out = append(out, row)
	}
	return out, nil
}
