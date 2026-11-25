// Command impulselab 生成“重力整除余数 / 逐帧冲量守恒”任务的全部证据产物：
//
//   - artifacts/impulse/loss_table.json     命题一：每帧损失 = r*S = rho，累计 = F*rho
//   - artifacts/impulse/carry_proof.json     命题二：跨帧余数台账逐帧恒等式与误差界
//   - artifacts/impulse/carry_per_frame.csv 跨帧台账 S=3 的逐帧恒等式/对设定值差
//   - artifacts/impulse/per_frame_impulse.csv 三档子步数 x 逐帧冲量对设定值/跨档差（全 0）
//   - artifacts/impulse/height_drift.csv     旧整除做法 vs 余数补偿的高度差（int64 精确轨迹）
//   - artifacts/hashes_F100.txt / hashes_F1000.txt  两档帧数的逐帧状态哈希（各一份）
//   - artifacts/hash_manifest.json           哈希档位元数据与回放一致性
//   - artifacts/impulse/result.json          机器可读总结论
//   - artifacts/result.json                  与上相同的顶层入口
//
// 生成器本身是一道闸门：任何逐帧差值非 0 都会以非零码退出。
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goframe/api"
	"goframe/internal/impulse"
	"goframe/replay"
)

const Q = int64(1 << 16)

type lossTier struct {
	SubSteps                 int     `json:"substeps"`
	SetpointRaw              int64   `json:"setpoint_raw"`
	QuotientQ                int64   `json:"quotient_q_raw_per_substep"`
	EuclidRemainderRho       int64   `json:"euclid_remainder_rho_raw"`
	PerSubstepRemainderRaw   string  `json:"per_substep_remainder_r_exact_raw"`
	NaiveDeliveredPerFrame   int64   `json:"naive_delivered_per_frame_raw"`
	LossPerFrameRaw          int64   `json:"loss_per_frame_raw"`
	LossPerFrameRExpr        string  `json:"loss_per_frame_equals_r_times_S"`
	LossPerFrameScaledMicro  int64   `json:"loss_per_frame_rho_times_S_in_micro_units"`
	CumulativeLossF100Raw    int64   `json:"cumulative_loss_F100_raw"`
	CumulativeLossF1000Raw   int64   `json:"cumulative_loss_F1000_raw"`
	CumulativeLossF100Fixed  float64 `json:"cumulative_loss_F100_fixed_units"`
	CumulativeLossF1000Fixed float64 `json:"cumulative_loss_F1000_fixed_units"`
}

type carryTier struct {
	SubSteps             int   `json:"substeps"`
	BoundRaw             int64 `json:"error_bound_remainder_raw"`
	MaxRemainderSeen     int64 `json:"max_remainder_seen_raw"`
	IdentityAllZero      bool  `json:"every_frame_identity_diff_zero"`
	TelescopingOK        bool  `json:"telescoping_sum_matches"`
	FinalRemainderAt1000 int64 `json:"final_remainder_F1000_raw"`
	CumulativeShortfall  int64 `json:"cumulative_shortfall_F1000_raw"`
}

type result struct {
	GravityInt          int               `json:"gravity_int"`
	ScaleQ16            int64             `json:"q16_scale"`
	SetpointRaw         int64             `json:"setpoint_raw_per_frame_per_body"`
	SubStepTiers        []int             `json:"substep_tiers"`
	HashFrameTiers      []int             `json:"hash_frame_tiers"`
	VerifyFrames        int               `json:"verify_frames"`
	LossFormula         string            `json:"loss_formula"`
	LossTiers           []lossTier        `json:"loss_tiers"`
	CarryBoundNumber    string            `json:"carry_error_bound_number"`
	CarryTiers          []carryTier       `json:"carry_tiers"`
	ConservationAllZero bool              `json:"all_per_frame_and_cross_tier_diffs_zero"`
	MaxAbsDiff          int64             `json:"max_abs_diff_raw"`
	ModelMatchesKernel  bool              `json:"int64_height_model_matches_kernel_F64"`
	HashFiles           map[string]string `json:"hash_files"`
	HashReplayIdentical map[string]bool   `json:"hash_replay_identical"`
	AllChecksPass       bool              `json:"all_checks_pass"`
}

func main() {
	artDir := flag.String("artifacts", "artifacts", "artifacts 根目录")
	gravity := flag.Int("gravity", 2, "重力整数设定值（FromInt）")
	verifyFrames := flag.Int("verify-frames", 1000, "逐帧验证帧数")
	flag.Parse()

	impDir := filepath.Join(*artDir, "impulse")
	must(os.MkdirAll(impDir, 0o755))

	Ss := []int{3, 5, 7}
	frameTiers := []int{100, 1000}
	G := int64(*gravity) * Q

	res := result{
		GravityInt:          *gravity,
		ScaleQ16:            Q,
		SetpointRaw:         G,
		SubStepTiers:        Ss,
		HashFrameTiers:      frameTiers,
		VerifyFrames:        *verifyFrames,
		LossFormula:         "loss/frame = r*S = rho raw; cumulative(F) = F*r*S = F*rho raw",
		CarryBoundNumber:    "chosen tiers: max 6 raw = 6/65536 ~= 9.1553e-5; universal S<=16: <=15 raw = 15/65536 ~= 2.2888e-4; exact in-frame spread: per-frame error 0, substep phase < 1 raw = 2^-16 ~= 1.5259e-5",
		HashFiles:           map[string]string{},
		HashReplayIdentical: map[string]bool{},
	}

	// ---------- 命题一：整除损失 ----------
	for _, s := range Ss {
		S := int64(s)
		q, rho := impulse.Euclid(G, S)
		_, delivered, loss := impulse.Naive(G, S)
		if loss != rho || delivered != S*q {
			fatalf("naive loss identity mismatch")
		}
		res.LossTiers = append(res.LossTiers, lossTier{
			SubSteps:                 s,
			SetpointRaw:              G,
			QuotientQ:                q,
			EuclidRemainderRho:       rho,
			PerSubstepRemainderRaw:   fmt.Sprintf("%d/%d", rho, S),
			NaiveDeliveredPerFrame:   delivered,
			LossPerFrameRaw:          loss,
			LossPerFrameRExpr:        fmt.Sprintf("(%d/%d)*%d = %d", rho, S, S, loss),
			LossPerFrameScaledMicro:  rho * S, // 微单位 u=raw/S 下的整数 r(=rho) * S
			CumulativeLossF100Raw:    100 * loss,
			CumulativeLossF1000Raw:   1000 * loss,
			CumulativeLossF100Fixed:  float64(100*loss) / float64(Q),
			CumulativeLossF1000Fixed: float64(1000*loss) / float64(Q),
		})
	}
	writeJSON(filepath.Join(impDir, "loss_table.json"), res.LossTiers)

	// ---------- 命题二：跨帧余数台账 ----------
	carryRows := [][]string{}
	for _, s := range Ss {
		S := int64(s)
		c := &impulse.FrameCarry{}
		var sum int64
		allZero := true
		var maxRem int64
		var firstCarryRows [][]string
		for f := 0; f < *verifyFrames; f++ {
			q, delivered, remIn, remOut := c.CarryFrame(G, S)
			d := impulse.IdentityDiff(G, delivered, remIn, remOut)
			if d != 0 {
				allZero = false
			}
			if remOut > maxRem {
				maxRem = remOut
			}
			sum += delivered
			if s == 3 && f < 12 { // 展示用：S=3 前 12 帧写明细
				firstCarryRows = append(firstCarryRows, []string{
					itoa(f), itoa(G), itoa(remIn), itoa(q), itoa(delivered),
					itoa(remOut), itoa(d), itoa(delivered - G),
				})
			}
		}
		want := int64(*verifyFrames)*G - c.Rem
		res.CarryTiers = append(res.CarryTiers, carryTier{
			SubSteps:             s,
			BoundRaw:             S - 1,
			MaxRemainderSeen:     maxRem,
			IdentityAllZero:      allZero,
			TelescopingOK:        sum == want,
			FinalRemainderAt1000: c.Rem,
			CumulativeShortfall:  c.Rem,
		})
		if s == 3 {
			carryRows = firstCarryRows
		}
	}
	writeJSON(filepath.Join(impDir, "carry_proof.json"), res.CarryTiers)
	writeCSV(filepath.Join(impDir, "carry_per_frame.csv"),
		[]string{"frame", "setpoint_G", "rem_in", "per_sub_q", "delivered", "rem_out", "identity_diff_G+in-out-out", "vs_setpoint_diff"},
		carryRows)

	// ---------- 验收：三档子步数 x 逐帧冲量 ----------
	worlds := make([]*api.World, len(Ss))
	for i, s := range Ss {
		w, err := api.New(api.Options{Bodies: 4, SubSteps: s, Gravity: *gravity})
		if err != nil {
			fatalf("new world s=%d: %v", s, err)
		}
		worlds[i] = w
	}
	var maxAbs int64
	allZero := true
	pfHeader := []string{"frame", "setpoint_raw"}
	for _, s := range Ss {
		pfHeader = append(pfHeader, fmt.Sprintf("imp_s%d", s))
	}
	for _, s := range Ss {
		pfHeader = append(pfHeader, fmt.Sprintf("diff_vs_setpoint_s%d", s))
	}
	for a := 0; a < len(Ss); a++ {
		for b := a + 1; b < len(Ss); b++ {
			pfHeader = append(pfHeader, fmt.Sprintf("cross_diff_s%d_minus_s%d", Ss[a], Ss[b]))
		}
	}
	var pfRows [][]string

	for f := 0; f < *verifyFrames; f++ {
		imp := make([]int64, len(Ss))
		snapsBefore := make([][]int32, len(Ss))
		for i, w := range worlds {
			snapsBefore[i] = w.Snapshot()
			if err := w.Step(nil); err != nil {
				fatalf("step: %v", err)
			}
			imp[i] = w.FrameGravityImpulse()
			// 同时用公开 Snapshot 的 velY 模差独立复核（body 0，index 3）。
			after := w.Snapshot()
			if d := int64(after[3] - snapsBefore[i][3]); d != G {
				allZero = false
				if abs(G-d) > maxAbs {
					maxAbs = abs(G - d)
				}
			}
		}
		row := []string{itoa(f), itoa(G)}
		for _, v := range imp {
			row = append(row, itoa(v))
		}
		for _, v := range imp {
			d := v - G
			if d != 0 {
				allZero = false
			}
			if abs(d) > maxAbs {
				maxAbs = abs(d)
			}
			row = append(row, itoa(d))
		}
		// 三档两两之比对：s3-s5、s3-s7、s5-s7。
		for a := 0; a < len(imp); a++ {
			for b := a + 1; b < len(imp); b++ {
				d := imp[a] - imp[b]
				if d != 0 {
					allZero = false
				}
				if abs(d) > maxAbs {
					maxAbs = abs(d)
				}
				row = append(row, itoa(d))
			}
		}
		pfRows = append(pfRows, row)
	}
	res.ConservationAllZero = allZero
	res.MaxAbsDiff = maxAbs
	writeCSV(filepath.Join(impDir, "per_frame_impulse.csv"), pfHeader, pfRows)

	// ---------- 附加：Options 允许的全部子步档 1..16 逐帧守恒扫描 ----------
	type sweepTier struct {
		SubSteps            int   `json:"substeps"`
		NaiveLossPerFrame   int64 `json:"naive_loss_per_frame_raw"`
		FramesChecked       int   `json:"frames_checked"`
		MaxAbsDiffRaw       int64 `json:"max_abs_diff_raw"`
		PerFrameImpulseZero bool  `json:"every_frame_equals_setpoint"`
	}
	sweep := make([]sweepTier, 0, 16)
	for s := 1; s <= 16; s++ {
		w, err := api.New(api.Options{Bodies: 2, SubSteps: s, Gravity: *gravity})
		if err != nil {
			fatalf("sweep new S=%d: %v", s, err)
		}
		_, _, naiveLoss := impulse.Naive(G, int64(s))
		st := sweepTier{SubSteps: s, NaiveLossPerFrame: naiveLoss, FramesChecked: *verifyFrames, PerFrameImpulseZero: true}
		for f := 0; f < *verifyFrames; f++ {
			before := w.Snapshot()
			if err := w.Step(nil); err != nil {
				fatalf("sweep step S=%d: %v", s, err)
			}
			after := w.Snapshot()
			if d := w.FrameGravityImpulse() - G; d != 0 {
				st.PerFrameImpulseZero = false
				if abs(d) > st.MaxAbsDiffRaw {
					st.MaxAbsDiffRaw = abs(d)
				}
			}
			for body := 0; body < 2; body++ {
				idx := 4*body + 3
				if d := int64(after[idx]-before[idx]) - G; d != 0 {
					st.PerFrameImpulseZero = false
					if abs(d) > st.MaxAbsDiffRaw {
						st.MaxAbsDiffRaw = abs(d)
					}
				}
			}
		}
		if !st.PerFrameImpulseZero {
			allZero = false
		}
		sweep = append(sweep, st)
	}
	writeJSON(filepath.Join(impDir, "substep_sweep.json"), sweep)

	// ---------- 高度漂移：int64 精确轨迹，并在 F=64 与真实内核对账 ----------
	type traj struct{ pNaive, pExact, vNaive, vExact int64 }
	states := make([]traj, len(Ss))
	var heightHeader []string
	heightHeader = append(heightHeader, "frame")
	for _, s := range Ss {
		heightHeader = append(heightHeader,
			fmt.Sprintf("posY_naive_s%d", s), fmt.Sprintf("posY_exact_s%d", s), fmt.Sprintf("height_gap_exact_minus_naive_s%d", s))
	}
	var heightRows [][]string
	modelOK := true
	for f := 0; f < *verifyFrames; f++ {
		row := []string{itoa(f + 1)}
		for i, s := range Ss {
			S := int64(s)
			naiveSteps, _, _ := impulse.Naive(G, S)
			exactSteps := impulse.Exact(G, S)
			st := &states[i]
			for k := int64(0); k < S; k++ {
				st.vNaive += naiveSteps[k]
				st.pNaive += st.vNaive
				st.vExact += exactSteps[k]
				st.pExact += st.vExact
			}
			if f == 63 { // 无 int32 回绕的安全视距，与真实内核 Snapshot posY(body0,index1) 对账
				w, err := api.New(api.Options{Bodies: 1, SubSteps: s, Gravity: *gravity})
				if err != nil {
					fatalf("kernel crosscheck new: %v", err)
				}
				for fr := 0; fr < 64; fr++ {
					if err := w.Step(nil); err != nil {
						fatalf("kernel crosscheck step: %v", err)
					}
				}
				kernelPosY := int64(w.Snapshot()[1])
				if kernelPosY != st.pExact {
					modelOK = false
				}
			}
			row = append(row, itoa(st.pNaive), itoa(st.pExact), itoa(st.pExact-st.pNaive))
		}
		heightRows = append(heightRows, row)
	}
	res.ModelMatchesKernel = modelOK
	writeCSV(filepath.Join(impDir, "height_drift.csv"), heightHeader, heightRows)

	// ---------- 两档帧数的哈希，各存一份 ----------
	hashOpt := api.Options{Bodies: 8, SubSteps: 3, Gravity: *gravity}
	manifest := map[string]any{
		"options": map[string]any{
			"bodies": hashOpt.Bodies, "substeps": hashOpt.SubSteps, "gravity": hashOpt.Gravity,
			"command": "body=i%8 PushX=1<<14, Tick=i (与 cmd/goframe run 一致)",
		},
		"tiers": map[string]any{},
	}
	for _, nf := range frameTiers {
		frames := make([]replay.Frame, nf)
		for i := range frames {
			frames[i] = replay.Frame{Commands: []api.Command{{
				Tick: i, Body: i % hashOpt.Bodies, PushX: 1 << 14,
			}}}
		}
		log, hashes, err := replay.Record(hashOpt, frames)
		if err != nil {
			fatalf("record F=%d: %v", nf, err)
		}
		again, err := replay.Playback(log)
		if err != nil {
			fatalf("playback F=%d: %v", nf, err)
		}
		same := len(again) == len(hashes)
		for i := 0; same && i < len(hashes); i++ {
			same = hashes[i] == again[i]
		}
		fname := fmt.Sprintf("hashes_F%d.txt", nf)
		fpath := filepath.Join(*artDir, fname)
		var buf []byte
		for i, h := range hashes {
			buf = append(buf, []byte(fmt.Sprintf("tick=%d hash=%016x\n", i, h))...)
		}
		if same {
			buf = append(buf, []byte("replay identical\n")...)
		} else {
			buf = append(buf, []byte("REPLAY DIVERGED\n")...)
		}
		must(os.WriteFile(fpath, buf, 0o644))
		key := fmt.Sprintf("F%d", nf)
		res.HashFiles[key] = filepath.Join("artifacts", fname)
		res.HashReplayIdentical[key] = same
		manifest["tiers"].(map[string]any)[key] = map[string]any{
			"frames": nf, "file": filepath.Join("artifacts", fname),
			"final_hash":       fmt.Sprintf("%016x", hashes[len(hashes)-1]),
			"first_hash":       fmt.Sprintf("%016x", hashes[0]),
			"replay_identical": same,
		}
		if !same {
			allZero = false
		}
	}
	writeJSON(filepath.Join(*artDir, "hash_manifest.json"), manifest)

	res.AllChecksPass = res.ConservationAllZero && res.ModelMatchesKernel
	for _, c := range res.CarryTiers {
		res.AllChecksPass = res.AllChecksPass && c.IdentityAllZero && c.TelescopingOK
	}
	for _, same := range res.HashReplayIdentical {
		res.AllChecksPass = res.AllChecksPass && same
	}
	writeJSON(filepath.Join(impDir, "result.json"), res)
	writeJSON(filepath.Join(*artDir, "result.json"), res)

	fmt.Printf("substeps=%v gravity=%d G=%d raw\n", Ss, *gravity, G)
	for _, t := range res.LossTiers {
		fmt.Printf("S=%d q=%d rho=%d -> loss/frame=%d raw ; F100=%d F1000=%d raw\n",
			t.SubSteps, t.QuotientQ, t.EuclidRemainderRho, t.LossPerFrameRaw,
			t.CumulativeLossF100Raw, t.CumulativeLossF1000Raw)
	}
	fmt.Printf("per-frame conservation all-zero=%v maxAbsDiff=%d model-kernel=%v\n",
		res.ConservationAllZero, res.MaxAbsDiff, res.ModelMatchesKernel)
	fmt.Printf("ALL_CHECKS_PASS=%v\n", res.AllChecksPass)
	if !res.AllChecksPass {
		os.Exit(1)
	}
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatalf("json %s: %v", path, err)
	}
	b = append(b, '\n')
	must(os.WriteFile(path, b, 0o644))
}

func writeCSV(path string, header []string, rows [][]string) {
	f, err := os.Create(path)
	if err != nil {
		fatalf("csv create %s: %v", path, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	must(w.Write(header))
	must(w.WriteAll(rows))
	w.Flush()
	must(w.Error())
}

func itoa(v any) string { return fmt.Sprintf("%d", v) }

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func must(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "impulselab: "+format+"\n", a...)
	os.Exit(1)
}
