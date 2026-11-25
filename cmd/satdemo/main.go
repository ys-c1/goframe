// Command satdemo 生成除数过零场景下旧除法（除零返回零）与带符号饱和除法的
// 轨迹与跳变对照数据，输出到指定目录的 CSV，供 docs/division-saturation.md 引用。
//
// 场景：某运动学量 v = a / b（如按反馈量归一化的速度修正项），每帧直接累加到位置。
// 除数 b 随时间线性扫过零，观察 b 过零帧两条轨迹的跳变。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goframe/internal/fixed"
)

const scale = int64(1) << 16

func q(realVal int) fixed.Value { return fixed.FromInt(realVal) }

// qHalf 表示步长 0.5 的 Q16.16 数，演示轨迹用。
func qHalf(v int) fixed.Value { return fixed.FromRaw(int(int64(v) * scale / 2)) }

func f32(v fixed.Value) float64 { return float64(v.Raw()) / float64(scale) }

func main() {
	out := flag.String("out", "artifacts", "CSV 输出目录")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "satdemo:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	sat := q(20) // 由速度最大量级 Vmax=20 反推的饱和阈值

	var traj strings.Builder
	traj.WriteString("tick,b,old_q,sat_q,old_pos,sat_pos\n")
	var oldPos, satPos fixed.Value
	// b 从 +4.0 以 0.5 为步长扫到 -4.0，共 17 个采样点，第 8 点恰为零。
	steps := 16
	a := q(4)
	for k := 0; k <= steps; k++ {
		b := qHalf(steps/2 - k) // k=0 -> +4.0, k=8 -> 0, k=16 -> -4.0
		oldQ := a.Div(b)
		satQ := a.SatDiv(b, sat)
		oldPos = oldPos.Add(oldQ)
		satPos = satPos.Add(satQ)
		fmt.Fprintf(&traj, "%d,%.4f,%.4f,%.4f,%.4f,%.4f\n",
			k, f32(b), f32(oldQ), f32(satQ), f32(oldPos), f32(satPos))
	}
	if err := writeFile(dir, "trajectory.csv", traj.String()); err != nil {
		return err
	}

	// 量化对照：固定被除数 a=2.0，常规除数、接近除零、精确除零与回绕区。
	// 每行给出商值，以及相对上一帧（|b| 更大、符号相同）的帧间跳变；
	// b=0 行的“上一帧”取同符号最近邻，单独度量过零处两侧的台阶。
	type row struct {
		label string
		b     fixed.Value
	}
	a2 := q(2)
	rows := []row{
		{"常规 b=+4.0", q(4)},
		{"常规 b=+1.0", q(1)},
		{"近零 b=+0.5", qHalf(1)},
		{"近零 b=+0.125", q(1).Div(q(8))},
		{"近零 b=+0.0625", q(1).Div(q(16))},
		{"近零 raw=2 (b≈3.05e-5)", fixed.FromRaw(2)},
		{"回绕 raw=1 (b≈1.53e-5)", fixed.FromRaw(1)},
		{"精确除零 b=0", fixed.Value(0)},
		{"近零 raw=-1 (b≈-1.53e-5)", fixed.FromRaw(-1)},
		{"近零 raw=-2 (b≈-3.05e-5)", fixed.FromRaw(-2)},
		{"近零 b=-0.0625", q(-1).Div(q(16))},
		{"近零 b=-0.125", q(-1).Div(q(8))},
		{"常规 b=-1.0", q(-1)},
	}
	var jt strings.Builder
	jt.WriteString("input,b_real,old_q,sat_q,old_jump_prev,sat_jump_prev\n")
	var oldPrev, satPrev fixed.Value
	first := true
	for _, r := range rows {
		oldQ := a2.Div(r.b)
		satQ := a2.SatDiv(r.b, sat)
		var oldJump, satJump fixed.Value
		if !first {
			oldJump = oldQ.Add(-oldPrev)
			satJump = satQ.Add(-satPrev)
		}
		fmt.Fprintf(&jt, "%s,%.6f,%.4f,%.4f,%.4f,%.4f\n",
			r.label, f32(r.b), f32(oldQ), f32(satQ), f32(oldJump), f32(satJump))
		oldPrev, satPrev = oldQ, satQ
		first = false
	}
	if err := writeFile(dir, "jump-table.csv", jt.String()); err != nil {
		return err
	}

	// 过零台阶的关键断言：旧除法无界（本例 +32 -> 0 -> -32），饱和除法被压在 2S 内。
	plus := a2.SatDiv(fixed.FromRaw(1), sat)
	minus := a2.SatDiv(fixed.FromRaw(-1), sat)
	cross := plus.Add(-minus)
	fmt.Printf("S=%.1f  饱和除法过零两侧台阶 |%+.1f -> %+.1f| = %.1f <= 2S=%.1f\n",
		f32(sat), f32(plus), f32(minus), f32(cross), 2*f32(sat))
	fmt.Printf("旧除法同点: %+.1f -> %.1f -> %+.1f（除零归零，符号信息丢失）\n",
		f32(a2.Div(fixed.FromRaw(1))), f32(a2.Div(0)), f32(a2.Div(fixed.FromRaw(-1))))
	fmt.Printf("CSV 已写入 %s/{trajectory,jump-table}.csv\n", dir)
	return nil
}

func writeFile(dir, name, content string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}
