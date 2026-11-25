// Command perturb 产出“全局随机源 vs 按实体派生子序列”的全部证据与指标。
//
// 子命令：
//
//	seq        生成一版扰动序列（global 旧做法 / derived 新做法）
//	matrix     证明全局源取值取决于种子与此前调用次数
//	avalanche  测量派生函数的雪崩率（输入翻 1 bit -> 输出翻转比例）
//	report     三档实体数的一致性 / 派生开销 / 差异分
//	compare    对比两份 seq 输出，给出失配率、汉明差异率等量化差异分
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"goframe/internal/rng"
)

const defaultSeed int64 = 20261126

type key struct{ tick, entity, axis int }

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "seq":
		code = runSeq(os.Args[2:])
	case "matrix":
		code = runMatrix(os.Args[2:])
	case "avalanche":
		code = runAvalanche(os.Args[2:])
	case "report":
		code = runReport(os.Args[2:])
	case "compare":
		code = runCompare(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: perturb <seq|matrix|avalanche|report|compare> [flags]")
}

func emit(path string, fn func(io.Writer) error) error {
	var w io.Writer = os.Stdout
	if path != "" {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	bw := bufio.NewWriter(w)
	if err := fn(bw); err != nil {
		return err
	}
	return bw.Flush()
}

func tagFor(axis int) rng.Tag {
	if axis == 0 {
		return rng.TagJitterX
	}
	return rng.TagJitterY
}

// buildCallOrder 构造指定调用次序下的 (tick, entity, axis) 调用列表。
func buildCallOrder(frames, bodies int, order string) ([]key, error) {
	keys := make([]key, 0, frames*bodies*2)
	switch order {
	case "frame": // 帧外实体中轴内：典型锁步遍历
		for t := 0; t < frames; t++ {
			for e := 0; e < bodies; e++ {
				keys = append(keys, key{t, e, 0}, key{t, e, 1})
			}
		}
	case "entity": // 实体外帧内
		for e := 0; e < bodies; e++ {
			for t := 0; t < frames; t++ {
				keys = append(keys, key{t, e, 0}, key{t, e, 1})
			}
		}
	case "shuffle":
		for t := 0; t < frames; t++ {
			for e := 0; e < bodies; e++ {
				keys = append(keys, key{t, e, 0}, key{t, e, 1})
			}
		}
		// 确定性 Fisher-Yates，置换本身也走派生函数，不引入 math/rand。
		for i := len(keys) - 1; i > 0; i-- {
			j := int(rng.Derive(0x5EED, i, rng.Tag(0x4F5244), 3) % uint64(i+1))
			keys[i], keys[j] = keys[j], keys[i]
		}
	default:
		return nil, fmt.Errorf("未知 --order=%q（可选 frame|entity|shuffle）", order)
	}
	return keys, nil
}

func runSeq(args []string) int {
	fs := flag.NewFlagSet("seq", flag.ContinueOnError)
	version := fs.String("version", "derived", "global（旧）| derived（新）")
	bodies := fs.Int("bodies", 8, "实体数")
	frames := fs.Int("frames", 12, "帧数")
	amp := fs.Int("amp", 1<<14, "扰动幅度（Q16.16 原始单位）")
	seed := fs.Int64("seed", defaultSeed, "主种子；global 版传 -1 使用进程随机自动播种的顶层全局源")
	order := fs.String("order", "frame", "调用次序 frame|entity|shuffle")
	out := fs.String("out", "", "输出文件，默认 stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bodies <= 0 || *frames <= 0 || *amp < 0 {
		fmt.Fprintln(os.Stderr, "bodies/frames 必须为正，amp 不能为负")
		return 2
	}
	if *version != "global" && *version != "derived" {
		fmt.Fprintln(os.Stderr, "--version 只能是 global 或 derived")
		return 2
	}

	call, err := buildCallOrder(*frames, *bodies, *order)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	vals := make([]uint64, *frames**bodies*2)
	var shared *rng.SharedSource
	if *version == "global" && *seed >= 0 {
		shared = rng.NewSharedSource(*seed)
	}
	for _, k := range call {
		idx := (k.tick**bodies+k.entity)*2 + k.axis
		var v uint64
		switch *version {
		case "derived":
			v = rng.Derive(uint64(*seed), k.entity, tagFor(k.axis), uint64(k.tick))
		case "global":
			if *seed < 0 {
				v = rng.GlobalUint64() // 顶层全局源：进程启动时随机播种
			} else {
				v = shared.Uint64() // 固定种子的共享源：仍受调用次数/次序支配
			}
		}
		vals[idx] = v
	}

	seedDesc := strconv.FormatInt(*seed, 10)
	if *version == "global" && *seed < 0 {
		seedDesc = "auto(process-random)"
	}
	err = emit(*out, func(w io.Writer) error {
		fmt.Fprintln(w, "# goframe perturbation sequence")
		fmt.Fprintf(w, "# version=%s seed=%s bodies=%d frames=%d axes=2 amp_raw=%d call_order=%s\n",
			*version, seedDesc, *bodies, *frames, *amp, *order)
		fmt.Fprintln(w, "# columns: tick,entity,axis,u64_hex,jitter_q16_raw")
		fmt.Fprintln(w, "tick,entity,axis,u64_hex,jitter_raw")
		for t := 0; t < *frames; t++ {
			for e := 0; e < *bodies; e++ {
				for ax := 0; ax < 2; ax++ {
					idx := (t**bodies+e)*2 + ax
					j := rng.MapJitter(vals[idx], int32(*amp)).Raw()
					axis := "X"
					if ax == 1 {
						axis = "Y"
					}
					fmt.Fprintf(w, "%d,%d,%s,%016x,%d\n", t, e, axis, vals[idx], j)
				}
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "write failed:", err)
		return 1
	}
	return 0
}

func runMatrix(args []string) int {
	fs := flag.NewFlagSet("matrix", flag.ContinueOnError)
	seedsFlag := fs.String("seeds", "1,42,20261126", "逗号分隔的种子")
	callsFlag := fs.String("calls", "0,1,2,3,5,8,13", "逗号分隔的此前调用次数 k")
	out := fs.String("out", "", "输出文件，默认 stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	seeds, err := parseInts64(*seedsFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad --seeds:", err)
		return 2
	}
	calls, err := parseInts(*callsFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad --calls:", err)
		return 2
	}

	valueAt := func(seed int64, preceding int) uint64 {
		g := rng.NewSharedSource(seed)
		for i := 0; i < preceding; i++ {
			g.Uint64()
		}
		return g.Uint64()
	}

	return writeEmit(out, func(w io.Writer) error {
		fmt.Fprintln(w, "# 证据一：同一个共享全局源里，“下一个值”同时由种子和此前调用次数决定")
		fmt.Fprintln(w, "# 单元格 = 以 seed 建源、空调 k 次后，下一个 Uint64 的高 16 位 hex")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%-4s", "k")
		for _, s := range seeds {
			fmt.Fprintf(w, "seed=%-12d", s)
		}
		fmt.Fprintln(w)
		for _, k := range calls {
			fmt.Fprintf(w, "%-4d", k)
			for _, s := range seeds {
				cell := fmt.Sprintf("%04x", uint16(valueAt(s, k)>>48))
				fmt.Fprintf(w, "%-17s", cell)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "# 证据二：实体 1 的首次扰动，被实体 0 的额外调用次数 m 平移（seed=7）")
		fmt.Fprintln(w, "# 这正是“别的实体多走一个随机分支，回放序列就错位”的机制")
		fmt.Fprintf(w, "%-28s%s\n", "entity0_extra_calls_m", "entity1_first_u64_hi16")
		for m := 0; m <= 5; m++ {
			g := rng.NewSharedSource(7)
			for i := 0; i < m; i++ {
				g.Uint64() // 实体 0 的额外调用
			}
			v := g.Uint64() // 实体 1 的第一次扰动
			fmt.Fprintf(w, "%-28d%04x\n", m, uint16(v>>48))
		}
		return nil
	})
}

func runAvalanche(args []string) int {
	fs := flag.NewFlagSet("avalanche", flag.ContinueOnError)
	n := fs.Int("samples", 20000, "随机元组样本数")
	out := fs.String("out", "", "输出文件，默认 stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *n <= 0 {
		return 2
	}

	measure := func(flip func(master *uint64, entity *int, counter *uint64, bit int)) float64 {
		var sum float64
		for i := 0; i < *n; i++ {
			master := rng.Derive(0xA1, i, rng.Tag(1), 11)
			entity := int(rng.Derive(0xA2, i, rng.Tag(2), 22) % 4096)
			counter := rng.Derive(0xA3, i, rng.Tag(3), 33)
			bit := i % 64
			h0 := rng.Derive(master, entity, rng.TagJitterX, counter)
			flip(&master, &entity, &counter, bit)
			h1 := rng.Derive(master, entity, rng.TagJitterX, counter)
			sum += float64(bits.OnesCount64(h0^h1)) / 64.0
		}
		return sum / float64(*n)
	}

	masterRate := measure(func(m *uint64, _ *int, _ *uint64, bit int) { *m ^= 1 << uint(bit) })
	entityRate := measure(func(_ *uint64, e *int, _ *uint64, bit int) {
		*e ^= 1 << uint(bit%12) // 实体编号实际使用的低位
	})
	counterRate := measure(func(_ *uint64, _ *int, c *uint64, bit int) { *c ^= 1 << uint(bit) })

	return writeEmit(out, func(w io.Writer) error {
		fmt.Fprintln(w, "# 雪崩检验：派生元组翻转 1 bit，输出 uint64 的平均翻转比例（理想 0.5000）")
		fmt.Fprintf(w, "samples=%d\n", *n)
		fmt.Fprintf(w, "flip_master_bit   = %.4f\n", masterRate)
		fmt.Fprintf(w, "flip_entity_bit   = %.4f\n", entityRate)
		fmt.Fprintf(w, "flip_counter_bit  = %.4f\n", counterRate)
		return nil
	})
}

// TierMetrics 是一档实体数的三项核心指标及辅助统计。
type TierMetrics struct {
	Bodies  int `json:"bodies"`
	Frames  int `json:"frames"`
	Items   int `json:"items"`
	Samples int `json:"independence_samples_per_entity"`

	OrderChecks int     `json:"order_checks"`
	OrderEqual  int     `json:"order_equal"`
	Consistency float64 `json:"consistency"`

	NSPerItem       float64 `json:"derive_ns_per_item_median"`
	WorkUS          float64 `json:"derive_one_sequence_us"`
	GlobalNSPerItem float64 `json:"global_rand_ns_per_item_median"`
	DerivedVsGlobal float64 `json:"derived_over_global_ratio"`

	DiffScore float64 `json:"diff_score_sign_hamming"`
	PairCount int     `json:"entity_pairs"`
	MaxAbsR   float64 `json:"max_abs_pearson_r"`
	MeanAbsR  float64 `json:"mean_abs_pearson_r"`
}

type reportJSON struct {
	MasterSeed int64         `json:"master_seed"`
	GoVersion  string        `json:"go_version"`
	Tiers      []TierMetrics `json:"tiers"`
}

func runReport(args []string) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	tiersFlag := fs.String("tiers", "8,64,512", "逗号分隔的实体数档位")
	frames := fs.Int("frames", 64, "每档使用的帧数")
	samples := fs.Int("samples", 4096, "每实体用于差异分/相关性的样本数（自动对齐到 64）")
	jsonOut := fs.String("json", "", "metrics.json 输出路径")
	mdOut := fs.String("md", "", "三栏 markdown 表输出路径")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tiers, err := parseInts(*tiersFlag)
	if err != nil || len(tiers) == 0 {
		fmt.Fprintln(os.Stderr, "bad --tiers:", err)
		return 2
	}
	if *frames <= 0 {
		return 2
	}
	s := *samples
	if s < 64 {
		s = 64
	}
	s = (s / 64) * 64

	rep := reportJSON{MasterSeed: defaultSeed, GoVersion: runtime.Version(), Tiers: []TierMetrics{}}
	var md strings.Builder
	fmt.Fprintln(&md, "| 实体数（档） | 序列一致性（三种调用次序） | 派生开销（中位数） | 差异分 D：实体子序列间符号位汉明率，理想 0.5000 |")
	fmt.Fprintln(&md, "|---:|---|---|---:|")

	for _, b := range tiers {
		m := measureTier(b, *frames, s)
		rep.Tiers = append(rep.Tiers, m)
		label := tierLabel(b)
		fmt.Fprintf(&md, "| %d（%s） | %.4f%%（%d/%d 项次一致；正序/实体序/打乱） | %.2f ns/项；单序列 %.2f µs（全局源 %.2f ns/项，派生/全局=%.2f×） | %.5f（|r|max=%.4f，均值 %.4f，%d 对） |\n",
			b, label, m.Consistency*100, m.OrderEqual, m.OrderChecks,
			m.NSPerItem, m.WorkUS, m.GlobalNSPerItem, m.DerivedVsGlobal,
			m.DiffScore, m.MaxAbsR, m.MeanAbsR, m.PairCount)
	}

	if *jsonOut != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(*jsonOut, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if err := emit(*mdOut, func(w io.Writer) error {
		_, err := io.WriteString(w, md.String())
		return err
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *mdOut == "" {
		fmt.Print(md.String())
	}
	return 0
}

func tierLabel(b int) string {
	switch {
	case b <= 16:
		return "小档"
	case b <= 128:
		return "中档"
	default:
		return "大档"
	}
}

var benchSink uint64

func measureTier(bodies, frames, samples int) TierMetrics {
	const master = uint64(defaultSeed)
	items := bodies * frames * 2

	gen := func(order string) []uint64 {
		keys, err := buildCallOrder(frames, bodies, order)
		if err != nil {
			panic(err)
		}
		vals := make([]uint64, items)
		for _, k := range keys {
			idx := (k.tick*bodies+k.entity)*2 + k.axis
			vals[idx] = rng.Derive(master, k.entity, tagFor(k.axis), uint64(k.tick))
		}
		return vals
	}
	ref := gen("frame")
	entityOrder := gen("entity")
	shuffleOrder := gen("shuffle")
	equal := 0
	for i := range ref {
		if ref[i] == entityOrder[i] {
			equal++
		}
		if ref[i] == shuffleOrder[i] {
			equal++
		}
	}

	deriveBatch := func() {
		for t := 0; t < frames; t++ {
			for e := 0; e < bodies; e++ {
				benchSink ^= rng.Derive(master, e, rng.TagJitterX, uint64(t))
				benchSink ^= rng.Derive(master, e, rng.TagJitterY, uint64(t))
			}
		}
	}
	globalBatch := func() {
		for i := 0; i < items; i++ {
			benchSink ^= rng.GlobalUint64()
		}
	}
	ns := medianNSPerItem(items, deriveBatch)
	gns := medianNSPerItem(items, globalBatch)

	words := samples / 64
	bitsByEntity := make([][]uint64, bodies)
	valsByEntity := make([][]float64, bodies)
	for e := 0; e < bodies; e++ {
		bitsByEntity[e] = make([]uint64, words)
		valsByEntity[e] = make([]float64, samples)
		for k := 0; k < samples; k++ {
			r := rng.Derive(master, e, rng.TagJitterX, uint64(k))
			if r&(1<<63) != 0 {
				bitsByEntity[e][k/64] |= 1 << uint(k%64)
			}
			valsByEntity[e][k] = float64(r>>11) / float64(1<<53)
		}
	}

	var diffBits int64
	pairs := 0
	for e := 0; e < bodies; e++ {
		for f := e + 1; f < bodies; f++ {
			for w := 0; w < words; w++ {
				diffBits += int64(bits.OnesCount64(bitsByEntity[e][w] ^ bitsByEntity[f][w]))
			}
			pairs++
		}
	}
	diffScore := float64(diffBits) / float64(int64(pairs)*int64(samples))

	// 取三个锚点实体对所有其他实体算 Pearson，去重。
	anchorSet := map[int]bool{0: true}
	if bodies/3 < bodies {
		anchorSet[bodies/3] = true
	}
	if 2*bodies/3 < bodies {
		anchorSet[2*bodies/3] = true
	}
	type pair [2]int
	seen := map[pair]bool{}
	var rs []float64
	for a := range anchorSet {
		for e := 0; e < bodies; e++ {
			if e == a {
				continue
			}
			p := pair{a, e}
			if p[0] > p[1] {
				p = pair{p[1], p[0]}
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			rs = append(rs, math.Abs(pearson(valsByEntity[a], valsByEntity[e])))
		}
	}
	maxR, sumR := 0.0, 0.0
	for _, v := range rs {
		if v > maxR {
			maxR = v
		}
		sumR += v
	}
	meanR := 0.0
	if len(rs) > 0 {
		meanR = sumR / float64(len(rs))
	}

	return TierMetrics{
		Bodies: bodies, Frames: frames, Items: items, Samples: samples,
		OrderChecks: 2 * items, OrderEqual: equal,
		Consistency: float64(equal) / float64(2*items),
		NSPerItem:   ns, WorkUS: ns * float64(items) / 1000.0,
		GlobalNSPerItem: gns, DerivedVsGlobal: ns / gns,
		DiffScore: diffScore, PairCount: pairs,
		MaxAbsR: maxR, MeanAbsR: meanR,
	}
}

// medianNSPerItem 每轮执行足够多次以摊薄计时噪声，取 7 轮中位数。
func medianNSPerItem(items int, batch func()) float64 {
	reps := int(math.Ceil(2_000_000.0 / float64(items)))
	if reps < 1 {
		reps = 1
	}
	samples := make([]float64, 0, 7)
	for trial := 0; trial < 7; trial++ {
		t0 := time.Now()
		for r := 0; r < reps; r++ {
			batch()
		}
		elapsed := time.Since(t0).Nanoseconds()
		samples = append(samples, float64(elapsed)/float64(int64(reps)*int64(items)))
	}
	sort.Float64s(samples)
	return samples[len(samples)/2]
}

func pearson(a, b []float64) float64 {
	var sa, sb float64
	for i := range a {
		sa += a[i]
		sb += b[i]
	}
	n := float64(len(a))
	ma, mb := sa/n, sb/n
	var cov, va, vb float64
	for i := range a {
		x, y := a[i]-ma, b[i]-mb
		cov += x * y
		va += x * x
		vb += y * y
	}
	if va == 0 || vb == 0 {
		return 0
	}
	return cov / math.Sqrt(va*vb)
}

// ---- compare ----

type seqRow struct {
	tick, entity, axis int
	u                  uint64
	jitter             int64
}

func runCompare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	aPath := fs.String("a", "", "第一份 seq 文件")
	bPath := fs.String("b", "", "第二份 seq 文件")
	asJSON := fs.Bool("json", false, "输出单行 JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *aPath == "" || *bPath == "" {
		fmt.Fprintln(os.Stderr, "compare 需要 --a 与 --b")
		return 2
	}
	ampA, ra, err := readSeq(*aPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取 a 失败:", err)
		return 1
	}
	ampB, rb, err := readSeq(*bPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取 b 失败:", err)
		return 1
	}
	amp := ampA
	if amp == 0 {
		amp = ampB
	}

	n := len(ra)
	res := map[string]any{
		"a": *aPath, "b": *bPath, "items": n,
	}
	if len(rb) != n {
		res["length_mismatch"] = fmt.Sprintf("%d vs %d", n, len(rb))
	}
	limit := n
	if len(rb) < limit {
		limit = len(rb)
	}
	keyMismatch, valueMismatch := 0, 0
	var xorBits int64
	var absJitter int64
	for i := 0; i < limit; i++ {
		x, y := ra[i], rb[i]
		if x.tick != y.tick || x.entity != y.entity || x.axis != y.axis {
			keyMismatch++
		}
		if x.u != y.u {
			valueMismatch++
		}
		xorBits += int64(bits.OnesCount64(x.u ^ y.u))
		d := x.jitter - y.jitter
		if d < 0 {
			d = -d
		}
		absJitter += d
	}
	res["key_mismatches"] = keyMismatch
	res["value_mismatches"] = valueMismatch
	if n > 0 {
		res["mismatch_rate"] = float64(valueMismatch) / float64(n)
		res["u64_hamming_rate"] = float64(xorBits) / float64(64*int64(n))
		if amp > 0 {
			res["jitter_norm_mean_abs_diff"] = float64(absJitter) / float64(n) / float64(2*amp)
		}
	}
	res["identical"] = valueMismatch == 0 && keyMismatch == 0 && len(ra) == len(rb)

	if *asJSON {
		line, _ := json.Marshal(res)
		fmt.Println(string(line))
		return 0
	}
	fmt.Printf("compare %s  vs  %s\n", *aPath, *bPath)
	fmt.Printf("items=%d key_mismatches=%d value_mismatches=%d\n", n, keyMismatch, valueMismatch)
	if v, ok := res["mismatch_rate"]; ok {
		fmt.Printf("mismatch_rate=%.6f\n", v.(float64))
		fmt.Printf("u64_hamming_rate=%.6f (独立序列理想 0.5000)\n", res["u64_hamming_rate"].(float64))
		if j, ok := res["jitter_norm_mean_abs_diff"]; ok {
			fmt.Printf("jitter_norm_mean_abs_diff=%.6f (独立均匀序列理想 ≈0.3333, 复现一致为 0)\n", j.(float64))
		}
	}
	fmt.Printf("identical=%v\n", res["identical"])
	return 0
}

func readSeq(path string) (amp int, rows []seqRow, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			if strings.Contains(line, "amp_raw=") {
				for _, tok := range strings.Fields(line) {
					if strings.HasPrefix(tok, "amp_raw=") {
						amp, _ = strconv.Atoi(strings.TrimPrefix(tok, "amp_raw="))
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, "tick,") || strings.TrimSpace(line) == "" {
			continue
		}
		p := strings.Split(line, ",")
		if len(p) != 5 {
			return 0, nil, fmt.Errorf("坏行: %q", line)
		}
		t, _ := strconv.Atoi(p[0])
		e, _ := strconv.Atoi(p[1])
		ax := 0
		if p[2] == "Y" {
			ax = 1
		}
		u, err := strconv.ParseUint(p[3], 16, 64)
		if err != nil {
			return 0, nil, err
		}
		j, _ := strconv.ParseInt(p[4], 10, 64)
		rows = append(rows, seqRow{t, e, ax, u, j})
	}
	return amp, rows, sc.Err()
}

// ---- 小工具 ----

func writeEmit(out *string, fn func(io.Writer) error) int {
	if err := emit(*out, fn); err != nil {
		fmt.Fprintln(os.Stderr, "write failed:", err)
		return 1
	}
	return 0
}

func parseInts(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func parseInts64(s string) ([]int64, error) {
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
