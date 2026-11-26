package drift

import (
	"goframe/api"
	"goframe/internal/snapfmt"
)

// GridRow 是闭式标定图案 Δx_b=f·N、ΣΔx=f·N²/4 的一个格点。
type GridRow struct {
	Bodies          int   `json:"bodies"`
	N               int   `json:"N"`
	F               int   `json:"f"`
	PredDxRaw       int64 `json:"pred_dx_raw"`
	MeasDxBody0Raw  int64 `json:"meas_dx_body0_raw"`
	PredTotalXRaw   int64 `json:"pred_total_x_raw"`
	MeasTotalXRaw   int64 `json:"meas_total_x_raw"`
	Exact           bool  `json:"exact"`
	WholeUnitOnsetF int64 `json:"whole_unit_onset_f"`
}

// BuildCalibrationGrid 构造标定状态：每个实体 posY=bswap32(N)，其余为 0。
// naive 解读后每个 velX_b=N、其余为 0；S=1、gravity=0、无指令，
// 因而实测 Δx_b(f)=f·N，Σ_b=f·N²/4。测量全部走真实内核恢复+步进。
func BuildCalibrationGrid() ([]GridRow, error) {
	Bs := []int{1, 2, 4, 8, 16}
	Fs := []int{0, 1, 2, 8, 16, 32, 48, 64}
	var rows []GridRow
	for _, B := range Bs {
		N := 4 * B
		c := make([]int32, N)
		// posY = bswap32(N)：旧序大端落盘后，naive 小端解读会把它还原成 N，
		// 且因整段反转，该值恰好落到每个实体的 velX 字段。
		bswapN := int32(reverseBytes32(uint32(N)))
		for b := 0; b < B; b++ {
			c[4*b+1] = bswapN
		}
		snap := snapfmt.Snap{Bodies: B, SubSteps: 1, Gravity: 0, Tick: 0, Hash: 0, Raw: c}
		legacy := snapfmt.EncodeLegacyV1(snap)
		naive, err := snapfmt.DecodeLegacyV1Naive(legacy)
		if err != nil {
			return nil, err
		}
		for _, f := range Fs {
			opt := api.Options{Bodies: B, SubSteps: 1, Gravity: 0}
			w, err := api.Restore(opt, 0, naive.Raw)
			if err != nil {
				return nil, err
			}
			for i := 0; i < f; i++ {
				if err := w.Step(nil); err != nil {
					return nil, err
				}
			}
			raw := w.Snapshot()
			var measTotal int64
			for b := 0; b < B; b++ {
				measTotal += int64(raw[4*b+0])
			}
			measDx := int64(raw[0])
			predDx := int64(f * N)
			predTotal := int64(f) * int64(N) * int64(N) / 4
			rows = append(rows, GridRow{
				Bodies: B, N: N, F: f,
				PredDxRaw:       predDx,
				MeasDxBody0Raw:  measDx,
				PredTotalXRaw:   predTotal,
				MeasTotalXRaw:   measTotal,
				Exact:           measDx == predDx && measTotal == predTotal,
				WholeUnitOnsetF: (65536 + int64(N) - 1) / int64(N),
			})
		}
	}
	return rows, nil
}

func reverseBytes32(v uint32) uint32 {
	return ((v & 0x000000ff) << 24) |
		((v & 0x0000ff00) << 8) |
		((v & 0x00ff0000) >> 8) |
		((v & 0xff000000) >> 24)
}
