package sim

import (
	"math"
	"testing"

	"goframe/internal/fixed"
)

// TestComponentwiseProjectileErrorRatio 验证分量积分的主导误差只落在重力
// 分量方向：gx=0 时水平误差为 0，竖直误差为 -N*g*h^2/2 的 Q16.16 表示。
func TestComponentwiseProjectileErrorRatio(t *testing.T) {
	const (
		q      = 65536
		dtRaw  = 2048 // 1/32
		gRaw   = -10 * q
		y0Raw  = q
		v12Raw = 12 * q
		v45Raw = 556096 // 最接近 12/sqrt(2) 且能被 32 整除的 Q16 raw
	)

	cases := []struct {
		name        string
		vx, vy      int32
		steps       int
		wantYErrRaw int32
	}{
		{"horizontal", v12Raw, 0, 14, -4480},
		{"forty_five", v45Raw, v45Raw, 58, -18560}, // -320*N；另有 Q16 位移截断
		{"vertical", 0, v12Raw, 79, -25280},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStateWithDT(1, fixed.FromRaw(gRaw), fixed.FromRaw(dtRaw))
			s.SetLaunch(0, 0, fixed.FromRaw(y0Raw), fixed.FromRaw(int(tc.vx)), fixed.FromRaw(int(tc.vy)))
			for i := 0; i < tc.steps; i++ {
				s.IntegrateSemiImplicit(1)
			}

			x, y := s.Position(0)
			dt := float64(dtRaw) / q
			time := float64(tc.steps) * dt
			vx0 := float64(tc.vx) / q
			vy0 := float64(tc.vy) / q

			wantX := vx0 * time
			wantY := 1 + vy0*time - 5*time*time
			gotX := float64(x.Raw()) / q
			gotY := float64(y.Raw()) / q
			dx := gotX - wantX
			dy := gotY - wantY

			// 45° 的初速不是 Q16 可精确表示值，逐次乘法会累积约 49 raw
			// 的水平截断；这仍比竖直主导误差小两个数量级。
			if math.Abs(dx) > 64.0/q {
				t.Fatalf("水平误差过大: dx=%.9f", dx)
			}
			// Q16 逐次 Mul 会给 45 度分量带来最多约 50 raw 的截断累积，
			// 因此这里检查主导项的量级和符号，而不是要求逐 raw 相等。
			if math.Abs(dy-float64(tc.wantYErrRaw)/q) > 80.0/q {
				t.Fatalf("竖直误差=%d raw (%.9f), 期望主导项 %d raw", y.Raw()-int32(math.Round(wantY*q)), dy, tc.wantYErrRaw)
			}
			if dx != 0 && math.Abs(dx/dy) > 0.01 {
				t.Fatalf("水平/竖直误差比=%.6f, 应接近 gx/gy=0", math.Abs(dx/dy))
			}
		})
	}
}

// TestExactComponentResidualBelowLSB 验证正式 Integrate 使用常加速度分量
// 精确式后，在 Q16 可精确表示的实验参数下，整数帧状态与解析解逐 raw 相同。
func TestExactComponentResidualBelowLSB(t *testing.T) {
	const (
		q      = 65536
		dtRaw  = 2048
		gRaw   = -10 * q
		y0Raw  = q
		v12Raw = 12 * q
		v45Raw = 556096
	)
	cases := []struct {
		name   string
		vx, vy int32
		steps  int
	}{
		{"horizontal", v12Raw, 0, 14},
		{"forty_five", v45Raw, v45Raw, 58},
		{"vertical", 0, v12Raw, 79},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStateWithDT(1, fixed.FromRaw(gRaw), fixed.FromRaw(dtRaw))
			s.SetLaunch(0, 0, fixed.FromRaw(y0Raw), fixed.FromRaw(int(tc.vx)), fixed.FromRaw(int(tc.vy)))
			for i := 0; i < tc.steps; i++ {
				s.Integrate(1)
			}
			x, y := s.Position(0)
			time := float64(tc.steps) / 32.0
			wantX := float64(tc.vx) / q * time
			wantY := 1 + float64(tc.vy)/q*time - 5*time*time
			if math.Abs(float64(x.Raw())/q-wantX) > 1.0/q {
				t.Fatalf("x residual %d raw", x.Raw()-int32(math.Round(wantX*q)))
			}
			if math.Abs(float64(y.Raw())/q-wantY) > 1.0/q {
				t.Fatalf("y residual %d raw", y.Raw()-int32(math.Round(wantY*q)))
			}
		})
	}
}

// TestSubStepsScalePosition 防止旧缺陷回归：重力按子步分摊时，位移也必须
// 乘 1/sub；sub=4 的一帧总位移应接近 sub=1 的一帧，而不是变成四倍。
func TestSubStepsScalePosition(t *testing.T) {
	mk := func(sub int) *State {
		s := NewState(1, fixed.FromInt(10))
		s.SetLaunch(0, 0, fixed.FromInt(1), fixed.FromInt(2), fixed.FromInt(3))
		for i := 0; i < sub; i++ {
			s.Integrate(sub)
		}
		return s
	}

	one := mk(1)
	four := mk(4)
	x1, _ := one.Position(0)
	x4, _ := four.Position(0)
	// 水平速度不变；正确缩放后，一帧位移都是 2。允许 1 raw 舍入。
	if d := absRaw(x4.Raw() - x1.Raw()); d > 1 {
		t.Fatalf("sub=1 x=%d, sub=4 x=%d, 差 %d raw", x1.Raw(), x4.Raw(), d)
	}
}

func absRaw(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
