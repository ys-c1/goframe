// Command biaslab 复算“定点乘法取整偏置”实验。
//
// 它是全部数值结果的唯一事实来源（source of truth）：
//   - 定点结果直接调用 goframe/internal/fixed 的取整语义（与 fixed.go 同一套 / 与 >>）；
//   - 高精度参照用公共分母 S=2^16 的整数精确累加（等价 Fraction），无浮点误差；
//   - 产出 artifacts/bias/bias_frames.csv   开环乘加逐帧数据；
//     bounded_frames.csv        残差累加 / 反馈收缩的有界振荡数据；
//     summary.json              末态偏差、斜率、R²、界；
//     frame_hashes.txt          唯一一份逐帧指纹（hash.Values，与 State 同一 FNV）。
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"goframe/internal/hash"
)

const S = int64(1 << 16)

type roundMode int

const (
	modeTZ roundMode = iota
	modeFloor
)

func roundProduct(p int64, m roundMode) int64 {
	if m == modeTZ {
		// Go 的 / 对负数向零截断：trunc(p/S)。
		return p / S
	}
	// 有符号 >> 为算术右移，等价 floor(p/S)；用显式 floorDiv 表达同一语义。
	return floorDiv(p, S)
}

// floorDiv 返回 floor(a/b)，要求 b>0。
func floorDiv(a, b int64) int64 {
	q := a / b
	if r := a % b; r != 0 && r < 0 {
		q--
	}
	return q
}

// lcg 是固定参数的确定性生成器，保证样本可复现。
type lcg struct{ x uint32 }

func newLCG(seed uint32) *lcg { return &lcg{x: seed & 0x7fffffff} }

func (g *lcg) next() int {
	g.x = (1103515245*g.x + 12345) & 0x7fffffff
	return int(g.x)
}

func buildStreams(n int) map[string][]int64 {
	g := newLCG(20261126)
	mag := make([]int64, n)
	for i := range mag {
		mag[i] = int64(200 + g.next()%60000)
	}

	singlePos := append([]int64(nil), mag...)

	singleNeg := make([]int64, n)
	for i, a := range mag {
		singleNeg[i] = -a
	}

	// 严格同模配对：[+m0,-m0,+m1,-m1,...]，TZ 误差逐对精确抵消。
	paired := make([]int64, n)
	for k := 0; k < n/2; k++ {
		m := mag[k]
		paired[2*k] = m
		paired[2*k+1] = -m
	}

	// 伪随机正负各半（符号由后续 LCG 位决定，恰好 500/500）。
	prngHalf := make([]int64, n)
	for i, a := range mag {
		if g.next()&1 == 0 {
			prngHalf[i] = a
		} else {
			prngHalf[i] = -a
		}
	}

	// 确定性“正负各半”，但两侧余数均值不对称。系数 C=40503 ≡ 3 (mod 4)：
	//   +49152=+3S/4 的乘积余数为 S/4，TZ 误差 -1/4；
	//   -16384=-S/4  的幅度余数为 3S/4，TZ 误差 +3/4；
	// 每个 [+3S/4, -S/4] 两帧块净误差 +1/2。
	asymHalf := make([]int64, n)
	for k := 0; k < n/2; k++ {
		asymHalf[2*k] = 49152
		asymHalf[2*k+1] = -16384
	}

	return map[string][]int64{
		"single_pos":  singlePos,
		"single_neg":  singleNeg,
		"half_paired": paired,
		"half_prng":   prngHalf,
		"half_asym":   asymHalf,
	}
}

type macRow struct {
	tick     int
	caseName string
	modeName string
	aRaw     int64
	rounded  int64
	epsNum   int64 // eps = epsNum/S ULP
	fixedSum int64
	refNum   int64 // 精确参照累计的分子：refULP = refNum/S
	devNum   int64 // devULP = devNum/S
}

type curveStat struct {
	Case      string  `json:"case"`
	Mode      string  `json:"mode"`
	Pos       int     `json:"pos"`
	Neg       int     `json:"neg"`
	EndDevULP float64 `json:"endDevUlp"`
	EndDevNum int64   `json:"endDevNum"`
	MeanEps   float64 `json:"meanEpsPerFrame"`
	NPred     float64 `json:"nTimesMeanEps"`
	Slope     float64 `json:"slopeUlpPerFrame"`
	Intercept float64 `json:"intercept"`
	R2        float64 `json:"r2"`
}

// runMAC 计算一条开环“定点乘后取整 + 精确加法累加”曲线。
func runMAC(caseName, modeName string, vals []int64, C int64, m roundMode) ([]macRow, curveStat) {
	n := len(vals)
	rows := make([]macRow, 0, n)
	var fixedSum, refNum, epsSum int64
	var pos, neg int
	devs := make([]float64, n)

	for i, a := range vals {
		p := a * C
		r := roundProduct(p, m)
		epsNum := r*S - p
		epsSum += epsNum
		fixedSum += r
		refNum += p
		devNum := fixedSum*S - refNum
		if a > 0 {
			pos++
		} else if a < 0 {
			neg++
		}
		rows = append(rows, macRow{i, caseName, modeName, a, r, epsNum, fixedSum, refNum, devNum})
		devs[i] = float64(devNum) / float64(S)
	}

	slope, intercept, r2 := linfit(devs)
	st := curveStat{
		Case: caseName, Mode: modeName, Pos: pos, Neg: neg,
		EndDevULP: float64(rows[n-1].devNum) / float64(S),
		EndDevNum: rows[n-1].devNum,
		MeanEps:   float64(epsSum) / float64(n) / float64(S),
		Slope:     slope, Intercept: intercept, R2: r2,
	}
	st.NPred = float64(n) * st.MeanEps
	return rows, st
}

func linfit(y []float64) (slope, intercept, r2 float64) {
	n := len(y)
	var sx, sy, sxx, sxy float64
	for i, v := range y {
		x := float64(i)
		sx += x
		sy += v
		sxx += x * x
		sxy += x * v
	}
	nf := float64(n)
	den := nf*sxx - sx*sx
	slope = (nf*sxy - sx*sy) / den
	intercept = (sy - slope*sx) / nf
	mean := sy / nf
	var ssTot, ssRes float64
	for i, v := range y {
		d := v - mean
		ssTot += d * d
		r := v - (intercept + slope*float64(i))
		ssRes += r * r
	}
	if ssTot > 0 {
		r2 = 1 - ssRes/ssTot
	} else {
		r2 = 1
	}
	return
}

type boundedPoint struct {
	tick   int
	series string
	devULP float64
}

func main() {
	frames := flag.Int("frames", 1000, "帧数 N")
	out := flag.String("out", "artifacts/bias", "产物目录")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	N := *frames
	C := int64(40503) // 奇数系数，c≈0.618027，余数覆盖丰富

	streams := buildStreams(N)
	order := []string{"single_pos", "single_neg", "half_paired", "half_prng", "half_asym"}

	// ---- bias_frames.csv + stats ----
	csvPath := filepath.Join(*out, "bias_frames.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		fatal(err)
	}
	w := csv.NewWriter(f)
	must(w.Write([]string{"tick", "case", "mode", "aRaw", "rounded", "epsNum", "fixedSum", "refNum", "devNum", "devUlp"}))

	var stats []curveStat
	for _, cn := range order {
		for _, mm := range []struct {
			name string
			m    roundMode
		}{{"tz", modeTZ}, {"floor", modeFloor}} {
			rows, st := runMAC(cn, mm.name, streams[cn], C, mm.m)
			stats = append(stats, st)
			for _, r := range rows {
				must(w.Write([]string{
					fmt.Sprint(r.tick), r.caseName, r.modeName,
					fmt.Sprint(r.aRaw), fmt.Sprint(r.rounded), fmt.Sprint(r.epsNum),
					fmt.Sprint(r.fixedSum), fmt.Sprint(r.refNum), fmt.Sprint(r.devNum),
					fmt.Sprintf("%.9f", float64(r.devNum)/float64(S)),
				}))
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fatal(err)
	}
	f.Close()

	// ---- 唯一一份逐帧哈希：canonical = single_pos / tz ----
	hashPath := filepath.Join(*out, "frame_hashes.txt")
	hf, err := os.Create(hashPath)
	if err != nil {
		fatal(err)
	}
	canon, _ := runMAC("single_pos", "tz", streams["single_pos"], C, modeTZ)
	for _, r := range canon {
		raw := []int32{int32(r.fixedSum), int32(r.rounded)}
		h := hash.Values(r.tick, raw)
		if h2 := hash.Values(r.tick, raw); h != h2 { // 确定性自检
			fatal(fmt.Errorf("hash nondeterministic at tick %d", r.tick))
		}
		fmt.Fprintf(hf, "tick=%d hash=%016x\n", r.tick, h)
	}
	hf.Close()

	// ---- 有界振荡曲线 ----
	var bp []boundedPoint

	// (A) floor + 残差累加：dev = -R/S ∈ (-1,0]，硬界 1 ULP。
	{
		vals := streams["single_pos"]
		var res, refNum, ySum int64
		for i, a := range vals {
			p := a * C
			num := res + p
			y := floorDiv(num, S)
			res = num - y*S // 0 <= res < S
			ySum += y
			refNum += p
			dev := float64(ySum*S-refNum) / float64(S) // 恒等于 -res/S
			bp = append(bp, boundedPoint{i, "residue_floor", dev})
		}
	}

	// (B) 反馈收缩 z_{n+1}=R(c z_n)+u_n，c=15/16，硬界 Δ/(1-c)=15 ULP。
	Cb := int64(61440)
	for _, mm := range []struct {
		name string
		m    roundMode
	}{{"contract_floor", modeFloor}, {"contract_tz", modeTZ}} {
		var z int64
		ze := big.NewRat(0, 1)
		cRat := big.NewRat(Cb, S)
		for i := 0; i < N; i++ {
			u := int64(12000)
			if i%2 == 1 {
				u = -12000
			}
			z = roundProduct(z*Cb, mm.m) + u
			ze = new(big.Rat).Add(new(big.Rat).Mul(cRat, ze), big.NewRat(u, 1))
			dev, _ := new(big.Rat).Sub(big.NewRat(z, 1), ze).Float64()
			bp = append(bp, boundedPoint{i, mm.name, dev})
		}
	}

	bCSV := filepath.Join(*out, "bounded_frames.csv")
	bf, err := os.Create(bCSV)
	if err != nil {
		fatal(err)
	}
	bw := csv.NewWriter(bf)
	must(bw.Write([]string{"tick", "series", "devUlp"}))
	for _, p := range bp {
		must(bw.Write([]string{fmt.Sprint(p.tick), p.series, fmt.Sprintf("%.9f", p.devULP)}))
	}
	bw.Flush()
	if err := bw.Error(); err != nil {
		fatal(err)
	}
	bf.Close()

	resMin, resMax := seriesRange(bp, "residue_floor")
	cfMin, cfMax := seriesRange(bp, "contract_floor")
	ctMin, ctMax := seriesRange(bp, "contract_tz")

	summary := map[string]any{
		"frames":         N,
		"scale":          S,
		"coefficientRaw": C,
		"coefficient":    float64(C) / float64(S),
		"macCurves":      stats,
		"bounds": map[string]any{
			"residueFloor": map[string]any{
				"boundUlp": 1.0, "observedMin": resMin, "observedMax": resMax,
				"formula": "-1 < dev = -R/S <= 0, R = floor_mod(R+p,S) in [0,S); sup|dev| = 1 ULP",
			},
			"contract15over16": map[string]any{
				"c": 15.0 / 16.0, "deltaUlp": 15.0 / 16.0, "boundUlp": 15.0,
				"observedFloorMin": cfMin, "observedFloorMax": cfMax,
				"observedTZMin": ctMin, "observedTZMax": ctMax,
				"formula": "|e_N| <= c^N|e0| + delta(1-c^N)/(1-c) -> delta/(1-c) = (15/16)/(1/16) = 15 ULP",
			},
		},
		"hashFile":  filepath.Base(hashPath),
		"hashLines": N,
	}
	sj, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*out, "summary.json"), sj, 0o644); err != nil {
		fatal(err)
	}

	fmt.Printf("C=%d c=%.6f N=%d\n", C, float64(C)/float64(S), N)
	for _, st := range stats {
		fmt.Printf("%-12s %-5s +/-=%d/%d end=%9.2f N*meanEps=%9.2f slope=%+.5f R2=%.5f\n",
			st.Case, st.Mode, st.Pos, st.Neg, st.EndDevULP, st.NPred, st.Slope, st.R2)
	}
	fmt.Printf("residue_floor range [%.3f, %.3f] bound 1\n", resMin, resMax)
	fmt.Printf("contract floor range [%.3f, %.3f] bound 15; tz range [%.3f, %.3f]\n", cfMin, cfMax, ctMin, ctMax)
	fmt.Println("artifacts written to", *out)
}

func must(err error) {
	if err != nil {
		fatal(err)
	}
}

func seriesRange(ps []boundedPoint, series string) (mn, mx float64) {
	first := true
	for _, p := range ps {
		if p.series != series {
			continue
		}
		if first || p.devULP < mn {
			mn = p.devULP
		}
		if first || p.devULP > mx {
			mx = p.devULP
		}
		first = false
	}
	return
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "biaslab:", err)
	os.Exit(1)
}
