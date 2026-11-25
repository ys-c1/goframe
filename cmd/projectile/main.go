// Command projectile 复现重力定点量化误差实验，生成：
//
//	artifacts/projectile_metrics.json   全部指标（机器可读，含精确分数）
//	artifacts/projectile_tables.md      两条轨迹各自的三档对照表
//	artifacts/projectile_traces.csv     逐帧误差轨迹（审计二次增长）
//
// 并把 Markdown 表格打印到标准输出。
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"goframe/internal/projectile"
)

type metricsFile struct {
	GravityExact string                  `json:"gravity_exact"`
	Tolerance    string                  `json:"tolerance"`
	Note         string                  `json:"note"`
	Tiers        []projectile.Tier       `json:"tiers"`
	Trajectories []projectile.Trajectory `json:"trajectories"`
	Rows         []projectile.Row        `json:"rows"`
}

func main() {
	tolStr := flag.String("tolerance", "0.1", "落点容差 ε（十进制或分数，如 0.1 / 1/10）")
	outDir := flag.String("out", "artifacts", "产物输出目录")
	flag.Parse()

	eps, ok := new(big.Rat).SetString(*tolStr)
	if !ok {
		fmt.Fprintf(os.Stderr, "无法解析容差 %q\n", *tolStr)
		os.Exit(2)
	}

	tiers := projectile.DefaultTiers()
	trajs := projectile.DefaultTrajectories()
	rows := projectile.AnalyzeAll(eps, trajs, tiers)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}

	// JSON
	mf := metricsFile{
		GravityExact: "49/5 = 9.8",
		Tolerance:    eps.RatString(),
		Note:         "落点差为参考落地整数帧处的同帧纵向误差；横向偏移为插值落地点的射程差。",
		Tiers:        tiers,
		Trajectories: trajs,
		Rows:         rows,
	}
	writeJSON(filepath.Join(*outDir, "projectile_metrics.json"), mf)

	// CSV traces
	writeTraces(filepath.Join(*outDir, "projectile_traces.csv"), eps, trajs, tiers)

	// Markdown tables
	md := renderTables(eps, trajs, tiers, rows)
	writeFile(filepath.Join(*outDir, "projectile_tables.md"), md)

	fmt.Print(md)
}

func renderTables(eps *big.Rat, trajs []projectile.Trajectory, tiers []projectile.Tier, rows []projectile.Row) string {
	var b strings.Builder
	epsF, _ := eps.Float64()

	b.WriteString("# 重力 9.8 定点量化：三档分子位数 × 两条抛物轨迹\n\n")
	fmt.Fprintf(&b, "- 精确重力 g* = 49/5 = 9.8；落点容差 ε = %s（%s）。\n", eps.RatString(), fnum(epsF))
	b.WriteString("- 离散递推与内核一致（半隐式欧拉，SubSteps=1）：v += δg，y += v；" +
		"第 n 帧位置误差 |Δy_n| = |δg|·n(n+1)/2。\n\n")

	// 表 0：常量、δg 与二次系数
	b.WriteString("## 表 0　三档常量的分子、每帧加速度差与二次系数\n\n")
	b.WriteString("| 档位 | 分子 N（分子位数） | 近似 g=N/2^f | 每帧加速度差 δg（精确） | δg（小数） | 二次系数 c=|δg|/2（精确） | 连续帧限 √(2ε/|δg|) |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|\n")
	for _, t := range tiers {
		dg, _ := t.DGravity().Float64()
		b.WriteString(fmt.Sprintf("| %s（f=%d） | %d（%d 位） | %.10f | %s | %.6e | %s | %.1f |\n",
			t.Name, t.Bits, t.Numerator(), t.Digits(), t.Gravity(),
			t.DGravity().RatString(), dg, t.AbsCoeff().RatString(), t.FrameBudgetContinuous(eps)))
	}
	b.WriteString("\n")

	// 每条轨迹一份主表
	for ti, tr := range trajs {
		b.WriteString(fmt.Sprintf("## 表 %d　轨迹 %s：%s（H0=%d, vx=%d, vy=%d）\n\n",
			ti+1, tr.ID, tr.Title, tr.H0, tr.VX0, tr.VY0))
		b.WriteString("| 分子位数档 | 落点差 Δ=|Δy(n*)| | 容差 ε | 首次越界帧 k | 横向偏移 Δx（同帧分量） | 地面落点距=射程差 \\|ΔX\\|（对 ε） | 判定：Δ≤ε ⇔ n*<k |\n")
		b.WriteString("|---|---:|---:|---:|---:|---:|:---:|\n")
		for _, r := range rows[ti*len(tiers) : (ti+1)*len(tiers)] {
			verdict := "**过**"
			if !r.PassAtLanding {
				verdict = "**不过**"
			}
			gapCmp := "≤ε ✓"
			if !r.LandingGapPass {
				gapCmp = ">ε ✗"
			}
			relation := fmt.Sprintf("%d<%d", r.RefLandFrame, r.BreachFrame)
			if r.RefLandFrame >= r.BreachFrame {
				relation = fmt.Sprintf("%d≥%d", r.RefLandFrame, r.BreachFrame)
			}
			b.WriteString(fmt.Sprintf(
				"| %s（%d 位分子，n*=%d） | %s `%s` | %s | **%d** | %s | %s %s | %s（%s） |\n",
				r.Tier, r.NumDigits, r.RefLandFrame,
				fnum(r.LandDiffY), r.LandDiffYRat,
				fnum(epsF), r.BreachFrame,
				fnum(r.SameFrameDx),
				fnum(r.LandingGap), gapCmp,
				verdict, relation))
		}
		b.WriteString("\n")
		b.WriteString("- **横向偏移单列说明**：水平初速度为整数、各档精确表示，且重力误差只作用于竖直轴，" +
			"所以同帧分量 **Δx(n*)=0**（上表“横向偏移”列）。\n")
		b.WriteString("- 重力偏差改变落地时刻，比较各自**插值地面落点**会得到非零射程差 " +
			"ΔX=vx·(t_档−t_参考)，其直线落地距就是 `|ΔX|`（上表倒数第二列）。" +
			"它与“同帧位移差/首次越界帧”是两种口径：例如轨迹 B 的 Q16.16 在第 256 帧已中途越界、" +
			"n*=408 处 Δ=0.2546>ε，但插值地面落点距仅 0.0381≤ε。\n\n")
	}

	// 越界帧与容差一并核对
	b.WriteString("## 表 3　首次越界帧与容差的边界核对（两条轨迹共用同一定律）\n\n")
	b.WriteString("| 档位 | ε | k（首次 Δ>ε） | Δ(k−1) | 与 ε 比较 | Δ(k) | 与 ε 比较 | 核对 |\n")
	b.WriteString("|---|---:|---:|---:|:---:|---:|:---:|:---:|\n")
	for _, t := range tiers {
		k := t.BreachFrame(eps)
		e1, _ := t.ErrAtFrame(k - 1).Float64()
		e2, _ := t.ErrAtFrame(k).Float64()
		check := "✓"
		if !(e1 <= epsF && e2 > epsF) {
			check = "✗"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %d | %.10f | ≤ε | %.10f | >ε | %s |\n",
			t.Name, fnum(epsF), k, e1, e2, check))
	}
	b.WriteString("\n")

	// 逐档比对
	b.WriteString("## 逐档比对\n\n")
	b.WriteString("- 同一帧处落点差严格正比于 |δg|：**Δ_Q8 = 256·Δ_Q16**，**Δ_Q16 = 65536·Δ_Q32**。\n")
	b.WriteString("- 帧限 k∝1/√|δg|：位数 8→16 误差缩 256 倍、越界帧放大 √256=16 倍（16→256）；" +
		"16→32 误差缩 65536 倍、越界帧放大 256 倍（256→65536）。\n")
	for ti, tr := range trajs {
		rs := rows[ti*len(tiers) : (ti+1)*len(tiers)]
		b.WriteString(fmt.Sprintf("- 轨迹 %s（n*=%d）：", tr.ID, rs[0].RefLandFrame))
		parts := make([]string, 0, len(rs))
		for _, r := range rs {
			pass := "容差内"
			if !r.PassAtLanding {
				pass = "越界"
			}
			parts = append(parts, fmt.Sprintf("%s Δ=%s、同帧Δx=0、射程ΔX=%s → %s",
				r.Tier, fnum(r.LandDiffY), fnum(r.HorizontalDxRange), pass))
		}
		b.WriteString(strings.Join(parts, "；") + "。\n")
	}
	b.WriteString("\n")
	return b.String()
}

func writeTraces(path string, eps *big.Rat, trajs []projectile.Trajectory, tiers []projectile.Tier) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"traj", "tier", "frame", "y_tier", "y_ref", "abs_err", "tolerance", "within_tolerance"})
	epsF, _ := eps.Float64()
	for _, tr := range trajs {
		nMax := tr.ReferenceLanding().Frame
		for _, tier := range tiers {
			s := tier.Scale()
			y := int64(tr.H0 * s)
			vy := tr.VY0 * s
			for n := int64(0); n <= nMax; n++ {
				if n > 0 {
					vy -= tier.Numerator()
					y += vy
				}
				yTier := new(big.Rat).Quo(big.NewRat(y, 1), big.NewRat(s, 1))
				yRef := tr.RefY(n)
				errY := new(big.Rat).Abs(new(big.Rat).Sub(yTier, yRef))
				within := "1"
				if errY.Cmp(eps) > 0 {
					within = "0"
				}
				_ = w.Write([]string{
					tr.ID, tier.Name, strconv.FormatInt(n, 10),
					yTier.RatString(), yRef.RatString(), errY.RatString(),
					strconv.FormatFloat(epsF, 'g', -1, 64), within,
				})
			}
		}
	}
}

func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	writeFile(path, data)
}

func writeFile(path string, data any) {
	var b []byte
	switch d := data.(type) {
	case string:
		b = []byte(d)
	case []byte:
		b = d
	default:
		fatal(fmt.Errorf("unsupported write type"))
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fatal(err)
	}
}

func fnum(x float64) string {
	ax := x
	if ax < 0 {
		ax = -ax
	}
	if ax != 0 && (ax < 1e-3 || ax >= 1e7) {
		return strconv.FormatFloat(x, 'e', 6, 64)
	}
	return strconv.FormatFloat(x, 'f', 7, 64)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "projectile:", err)
	os.Exit(1)
}
