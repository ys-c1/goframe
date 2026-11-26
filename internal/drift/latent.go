package drift

import (
	"fmt"

	"goframe/api"
	"goframe/internal/snapfmt"
)

// LatentFrame 是「几十帧后整数位置才差 1 单位」样例的逐帧行。
type LatentFrame struct {
	F                   int    `json:"f"`
	Tick                int    `json:"tick"`
	DirectHash          string `json:"direct_hash"`
	NaiveHash           string `json:"naive_hash"`
	NormalizedHash      string `json:"normalized_hash"`
	HashEqualDirect     bool   `json:"hash_equal_direct"`
	NormalizedHashEqual bool   `json:"normalized_hash_equal_direct"`
	DxBody0Raw          int64  `json:"dx_body0_raw"`
	DxBody0UnitsExact   string `json:"dx_body0_units_exact"`
	XWholeUnitEqual     bool   `json:"x_whole_unit_equal"`
}

// LatentResult 是延迟可见漂移样例的全部数据。
type LatentResult struct {
	Bodies                 int           `json:"bodies"`
	N                      int           `json:"N"`
	BiasARaw               int64         `json:"bias_A_raw"`
	CheckpointTickLabel    int           `json:"checkpoint_tick_label"`
	HorizonFrames          int           `json:"horizon_frames"`
	HashOnsetTick          int           `json:"hash_onset_tick"`
	XWholeUnitOnsetF       int           `json:"x_whole_unit_onset_f"`
	XWholeUnitOnsetTick    int           `json:"x_whole_unit_onset_tick"`
	OnsetDeltaXRaw         int64         `json:"onset_delta_x_raw"`
	OnsetDeltaXWholeUnits  int64         `json:"onset_delta_x_whole_units"`
	NormalizedAllHashEqual bool          `json:"normalized_all_hash_equal"`
	LegacyFile             string        `json:"legacy_file"`
	CanonicalFile          string        `json:"canonical_file"`
	LegacySHA256           string        `json:"legacy_sha256,omitempty"`
	CanonicalSHA256        string        `json:"canonical_sha256,omitempty"`
	Frames                 []LatentFrame `json:"frames"`

	LegacyBytes    []byte `json:"-"`
	CanonicalBytes []byte `json:"-"`
}

// RunLatent 构造一份专门的旧序快照：每个实体 posY=8 单位，其余为 0。
// 经旧序大端 + naive 小端不反转解读后，每个 velX 恰好偏 A=2048 raw，
// 而 posX 载入误差为 0；gravity=0、无指令，于是 Δx(f)=f·2048 raw。
func RunLatent() (*LatentResult, error) {
	const B = 8
	const C = 48
	const extra = 40
	const A int64 = 2048

	N := 4 * B
	c := make([]int32, N)
	for b := 0; b < B; b++ {
		c[4*b+1] = 8 << 16 // posY = 8.0
	}
	opt := api.Options{Bodies: B, SubSteps: 1, Gravity: 0}

	// 直跑（从构造状态在 tick=C 恢复，然后空指令积分）。
	dw, err := api.Restore(opt, C, c)
	if err != nil {
		return nil, err
	}
	dh := make([]uint64, extra+1)
	dr := make([][]int32, extra+1)
	dh[0] = dw.Hash()
	dr[0] = append([]int32(nil), dw.Snapshot()...)
	for f := 1; f <= extra; f++ {
		if err := dw.Step(nil); err != nil {
			return nil, err
		}
		dh[f] = dw.Hash()
		dr[f] = append([]int32(nil), dw.Snapshot()...)
	}

	// 文件头哈希按旧样例留 0；逻辑哈希由内核现算。
	snap := snapfmt.Snap{Bodies: B, SubSteps: 1, Gravity: 0, Tick: C, Hash: 0, Raw: c}
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

	nw, err := api.Restore(opt, C, naiveSnap.Raw)
	if err != nil {
		return nil, err
	}
	rw, err := api.Restore(opt, C, normSnap.Raw)
	if err != nil {
		return nil, err
	}

	res := &LatentResult{
		Bodies: B, N: N, BiasARaw: A, CheckpointTickLabel: C, HorizonFrames: extra,
		HashOnsetTick: C,
		LegacyFile:    "latent-onset.old-order-v1.bin",
		CanonicalFile: "latent-onset.canonical-order-v2.bin",
		LegacyBytes:   legacy, CanonicalBytes: canonical,
	}

	normAll := true
	onsetF := -1
	for f := 0; f <= extra; f++ {
		if f > 0 {
			if err := nw.Step(nil); err != nil {
				return nil, err
			}
			if err := rw.Step(nil); err != nil {
				return nil, err
			}
		}
		nh, rh := nw.Hash(), rw.Hash()
		nraw := nw.Snapshot()
		dx := int64(nraw[0]) - int64(dr[f][0])
		normEq := rh == dh[f]
		if !normEq {
			normAll = false
		}
		wholeEq := nraw[0]>>16 == dr[f][0]>>16
		if !wholeEq && onsetF < 0 {
			onsetF = f
		}
		res.Frames = append(res.Frames, LatentFrame{
			F: f, Tick: C + f,
			DirectHash:          hx(dh[f]),
			NaiveHash:           hx(nh),
			NormalizedHash:      hx(rh),
			HashEqualDirect:     nh == dh[f],
			NormalizedHashEqual: normEq,
			DxBody0Raw:          dx,
			DxBody0UnitsExact:   fmt.Sprintf("%d/32", dx/A),
			XWholeUnitEqual:     wholeEq,
		})
	}
	res.NormalizedAllHashEqual = normAll
	if onsetF >= 0 {
		res.XWholeUnitOnsetF = onsetF
		res.XWholeUnitOnsetTick = C + onsetF
		res.OnsetDeltaXRaw = res.Frames[onsetF].DxBody0Raw
		res.OnsetDeltaXWholeUnits = res.OnsetDeltaXRaw >> 16
	}
	return res, nil
}

// VisibilityRow 是 A → 首次可见整单位帧的小表。
type VisibilityRow struct {
	VelocityBiasARaw    int64 `json:"velocity_bias_A_raw"`
	WholeUnitOnsetF     int   `json:"whole_unit_onset_f"`
	GlobalTickIfCheck48 int   `json:"global_tick_if_checkpoint48"`
	MeasDxAtOnsetRaw    int64 `json:"meas_dx_at_onset_raw"`
}

// VisibilityTable 给出三种速度偏差下的首次可见帧：f0=ceil(2^16/A)。
func VisibilityTable() []VisibilityRow {
	out := []VisibilityRow{}
	for _, A := range []int64{1024, 2048, 4096} {
		f0 := int((65536 + A - 1) / A)
		out = append(out, VisibilityRow{
			VelocityBiasARaw:    A,
			WholeUnitOnsetF:     f0,
			GlobalTickIfCheck48: 48 + f0,
			MeasDxAtOnsetRaw:    int64(f0) * A,
		})
	}
	return out
}
