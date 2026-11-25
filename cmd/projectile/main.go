// Command projectile 用确定性 Q16.16 分量积分内核生成三档发射角轨迹。
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

type launchCase struct {
	name string
	vx   int32
	vy   int32
}

func main() {
	out := flag.String("out", "artifacts/projectile_q16_traces.csv", "轨迹 CSV 输出路径")
	steps := flag.Int("steps", 96, "每个角度、每种格式导出的积分子步数")
	flag.Parse()

	if err := run(*out, *steps); err != nil {
		fmt.Fprintln(os.Stderr, "projectile failed:", err)
		os.Exit(1)
	}
}

func run(out string, steps int) error {
	if steps < 1 {
		return fmt.Errorf("steps 必须为正数: %d", steps)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"angle", "scheme", "step", "x_raw", "y_raw", "vx_raw", "vy_raw"}); err != nil {
		return err
	}

	const (
		q      = int32(1 << 16)
		dtRaw  = int32(2048) // 1/32 秒，Q16.16 可精确表示
		y0Raw  = q           // 1 米
		v12Raw = 12 * q
		v45Raw = 556096  // 最接近 12/sqrt(2) 且能被 dt 的 32 整除的 Q16 raw
		gRaw   = -10 * q // y 轴向上，重力为 -10 m/s^2
	)
	cases := []launchCase{
		{name: "horizontal", vx: v12Raw, vy: 0},
		{name: "forty_five", vx: v45Raw, vy: v45Raw},
		{name: "vertical", vx: 0, vy: v12Raw},
	}

	for _, tc := range cases {
		for _, scheme := range []string{"semi_implicit", "exact_component"} {
			st := sim.NewStateWithDT(1, fixed.FromRaw(int(gRaw)), fixed.FromRaw(int(dtRaw)))
			st.SetLaunch(0, 0, fixed.FromRaw(int(y0Raw)), fixed.FromRaw(int(tc.vx)), fixed.FromRaw(int(tc.vy)))

			for step := 0; step <= steps; step++ {
				x, y := st.Position(0)
				vx, vy := st.Velocity(0)
				rec := []string{
					tc.name,
					scheme,
					fmt.Sprintf("%d", step),
					fmt.Sprintf("%d", x.Raw()),
					fmt.Sprintf("%d", y.Raw()),
					fmt.Sprintf("%d", vx.Raw()),
					fmt.Sprintf("%d", vy.Raw()),
				}
				if err := w.Write(rec); err != nil {
					return err
				}
				if step < steps {
					if scheme == "semi_implicit" {
						st.IntegrateSemiImplicit(1)
					} else {
						st.Integrate(1)
					}
				}
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d steps x %d angles x 2 schemes)\n", out, steps, len(cases))
	return nil
}
