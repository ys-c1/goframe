package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"goframe/internal/clock"
)

// clockRun 生成三档帧率 × 两档帧数的长短帧对照数据：
// artifacts/clock_report.txt 是三行汇总报告，artifacts/clock_timeline.csv 是逐帧时间轴。
func clockRun(args []string) int {
	fs := flag.NewFlagSet("clock", flag.ContinueOnError)
	ratesFlag := fs.String("rates", "60,30,120", "目标帧率列表，逗号分隔")
	framesFlag := fs.String("frames", "60,300", "帧数列表，逗号分隔")
	outDir := fs.String("outdir", "artifacts", "输出目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rates, err := parseIntList(*ratesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad --rates:", err)
		return 2
	}
	counts, err := parseIntList(*framesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad --frames:", err)
		return 2
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir failed:", err)
		return 1
	}
	csvPath := filepath.Join(*outDir, "clock_timeline.csv")
	repPath := filepath.Join(*outDir, "clock_report.txt")

	csv, err := os.Create(csvPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create csv failed:", err)
		return 1
	}
	defer csv.Close()
	if _, err := fmt.Fprintln(csv,
		"rate_hz,frames,idx,frame_ms,kind,scheduled_ms,ideal_ms,drift_ms,drift_exact,fixed_ms,fixed_scheduled_ms,fixed_drift_exact"); err != nil {
		fmt.Fprintln(os.Stderr, "write csv failed:", err)
		return 1
	}

	var rep strings.Builder
	fmt.Fprintf(&rep, "帧长量化偏差对照报告（余数分配法，全部整数运算）\n")
	fmt.Fprintf(&rep, "生成帧数档：%v；帧率档：%v\n\n", counts, rates)

	// 第一部分：朴素固定帧长跑一分钟，偏差线性累积。
	rep.WriteString("一、朴素固定帧长（跑一分钟，理想帧率为目标帧率）\n")
	rep.WriteString("rateHz  固定帧长ms  每帧误差ms        一分钟帧数  一分钟累计偏差ms\n")
	allOK := true
	for _, r := range rates {
		fixedMS := 1000 / r
		framesPerMinute := 60 * r
		num, den, err := clock.FixedDriftMS(r, fixedMS, framesPerMinute)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fixed drift:", err)
			return 1
		}
		perFrameNum, perFrameDen, _ := clock.FixedDriftMS(r, fixedMS, 1)
		fmt.Fprintf(&rep, "%-7d %-10d %-18s %-11d %s\n",
			r, fixedMS, formatRat(perFrameNum, perFrameDen),
			framesPerMinute, formatRat(num, den))
	}
	rep.WriteString("结论：累计偏差 = 每帧误差 × 帧数，随帧数线性；调大帧长只改变误差符号与大小，不消除累积。\n\n")

	// 第二部分：余数分配，三行汇总（一行一档帧率），同时覆盖两档帧数。
	rep.WriteString("二、余数分配（长短帧）：偏差、界与帧数分布\n")
	rep.WriteString("rateHz  短/长帧长ms  周期帧  长:短  n=60 长/短帧  n=60端偏差/过程最大ms   n=300 长/短帧  n=300端偏差/过程最大ms  界\n")
	for _, r := range rates {
		s, err := clock.New(r)
		if err != nil {
			fmt.Fprintln(os.Stderr, "new scheduler:", err)
			return 1
		}
		cycle, _ := clock.CycleLen(r)
		lp, sp, _ := clock.LongShortRatio(r)
		ratio := fmt.Sprintf("%d:%d", lp, sp)
		perN := func(n int) string {
			long, short, _ := clock.Distribution(r, n)
			endNum, endDen, _ := clock.DriftMS(r, n)
			maxNum, maxDen, _ := clock.MaxAbsDriftMS(r, n)
			return fmt.Sprintf("%d/%d          %s/%s",
				long, short, formatRat(endNum, endDen), formatRat(maxNum, maxDen))
		}
		bound := fmt.Sprintf("|偏差| < 1帧 = %dms", s.ShortMS())
		fmt.Fprintf(&rep, "%-7d %-11s %-7d %-6s %-24s %-28s %s\n",
			r, fmt.Sprintf("%d/%d", s.ShortMS(), s.LongMS()),
			cycle, ratio, perN(60), perN(300), bound)

		// 逐帧时间轴：对每档帧数都完整输出，朴素固定帧长方案同步对照。
		for _, n := range counts {
			if err := writeTimeline(csv, r, n); err != nil {
				fmt.Fprintln(os.Stderr, "write timeline:", err)
				return 1
			}
			// 断言：过程中每个前缀偏差绝对值都小于一帧。
			maxNum, maxDen, _ := clock.MaxAbsDriftMS(r, n)
			if !lessThanFrame(maxNum, maxDen, s.ShortMS()) {
				allOK = false
			}
		}
	}

	// 第三部分：两档帧数的验收结论。
	rep.WriteString("\n三、验收（帧数 60 与 300 两档）\n")
	for _, r := range rates {
		s, _ := clock.New(r)
		for _, n := range counts {
			maxNum, maxDen, _ := clock.MaxAbsDriftMS(r, n)
			endNum, endDen, _ := clock.DriftMS(r, n)
			ok := lessThanFrame(maxNum, maxDen, s.ShortMS())
			if !ok {
				allOK = false
			}
			fmt.Fprintf(&rep, "rate=%dHz n=%d: 端偏差 %s ms，过程最大绝对偏差 %s ms，一帧=%dms -> %s\n",
				r, n, formatRat(endNum, endDen), formatRat(maxNum, maxDen),
				s.ShortMS(), passFail(ok))
		}
	}
	rep.WriteString("\n复算：任意第 idx 帧帧长 = FrameAt(rate, idx) = ⌊1000/rate⌋ + ⌊(idx+1)p/d⌋ − ⌊idx·p/d⌋，\n")
	rep.WriteString("其中 p/d = (1000 mod rate)/rate 的最简分数；测试已验证与状态机逐帧相等。\n")
	rep.WriteString("逐帧时间轴见 clock_timeline.csv（含朴素固定帧长方案的同步对照列）。\n")

	if err := os.WriteFile(repPath, []byte(rep.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write report failed:", err)
		return 1
	}
	fmt.Print(rep.String())
	fmt.Printf("\n写出 %s\n写出 %s\n", repPath, csvPath)
	if !allOK {
		fmt.Fprintln(os.Stderr, "验收失败：存在偏差不小于一帧的情形")
		return 1
	}
	return 0
}

// writeTimeline 把 rateHz 下前 n 帧的逐帧对照写入 CSV：
// 余数分配方案与朴素固定帧长方案并列，偏差均给出精确分数。
func writeTimeline(w *os.File, rateHz, n int) error {
	s, err := clock.New(rateHz)
	if err != nil {
		return err
	}
	fixedMS := 1000 / rateHz
	scheduled, fixedScheduled := 0, 0
	for i := 0; i < n; i++ {
		ms := s.Next()
		kind := "short"
		if ms == s.LongMS() && s.LongMS() != s.ShortMS() {
			kind = "long"
		}
		scheduled += ms
		fixedScheduled += fixedMS
		num, den, _ := clock.DriftMS(rateHz, i+1)
		fNum, fDen, _ := clock.FixedDriftMS(rateHz, fixedMS, i+1)
		idealNum, idealDen := (i+1)*1000, rateHz
		if _, err := fmt.Fprintf(w,
			"%d,%d,%d,%d,%s,%d,%s,%s,%d/%d,%d,%d,%d/%d\n",
			rateHz, n, i, ms, kind, scheduled,
			formatRat(idealNum, idealDen),
			formatRat(num, den), num, den,
			fixedMS, fixedScheduled, fNum, fDen); err != nil {
			return err
		}
	}
	return nil
}

// lessThanFrame 判断 |num/den| < frameMS（毫秒）。
func lessThanFrame(num, den, frameMS int) bool {
	if den <= 0 {
		return false
	}
	if num < 0 {
		num = -num
	}
	return num < frameMS*den
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// formatRat 把精确分数 num/den 格式化为 “符号整数.三位小数(=num/den)”，
// 小数部分用整数运算算出，内核与报告都不引入浮点。
func formatRat(num, den int) string {
	if den == 1 {
		return strconv.Itoa(num)
	}
	sign := ""
	a := num
	if a < 0 {
		sign = "-"
		a = -a
	}
	whole := a / den
	rem := a % den
	// 四舍五入到千分之一毫秒。
	frac := (rem*1000 + den/2) / den
	if frac >= 1000 {
		whole++
		frac -= 1000
	}
	return fmt.Sprintf("%s%d.%03d(=%d/%d)", sign, whole, frac, num, den)
}

func parseIntList(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("空列表")
	}
	return out, nil
}
