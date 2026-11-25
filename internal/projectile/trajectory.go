package projectile

import (
	"math/big"
)

// Trajectory 是一条抛物线的初始条件（y 轴向上，地面 y=0，初始高度 H0）。
// 初速度取整数世界单位/帧，因而在各档二进制定点下都被精确表示，
// 把误差来源严格隔离为重力常量的量化。
type Trajectory struct {
	ID    string
	Title string
	H0    int64 // 初始高度
	VX0   int64 // 水平初速度（恒定，无水平加速度）
	VY0   int64 // 竖直初速度（向上为正）
}

// DefaultTrajectories 给出两条形态不同的抛物线：
// A 低伸快弧约 61 帧落地，B 高抛长弧约 408 帧落地。
func DefaultTrajectories() []Trajectory {
	return []Trajectory{
		{ID: "A", Title: "低伸快弧", H0: 100, VX0: 900, VY0: 300},
		{ID: "B", Title: "高抛长弧", H0: 100, VX0: 300, VY0: 2000},
	}
}

// Landing 是一次落地事件：Frame 为首个 y<=0 的整数帧，
// Time 为帧间线性插值得到的精确落地时刻，X 为插值落点横坐标。
type Landing struct {
	Frame int64
	Time  *big.Rat
	X     *big.Rat
}

// RefY 是精确参考解第 n 帧的高度：
//
//	y_n = H0 + VY0·n − (49/10)·n(n+1)   （半隐式欧拉，g*=49/5）
func (tr Trajectory) RefY(n int64) *big.Rat {
	nn := new(big.Int).Mul(big.NewInt(n), big.NewInt(n+1))
	term := new(big.Rat).Mul(big.NewRat(49, 10), new(big.Rat).SetInt(nn))
	y := big.NewRat(tr.H0+tr.VY0*n, 1)
	return y.Sub(y, term)
}

// ReferenceLanding 用放大 10 倍的整数式精确寻找落地帧并做帧间插值。
func (tr Trajectory) ReferenceLanding() Landing {
	// Y10_n = 10·H0 + 10·VY0·n − 49·n(n+1)，与 10·y_n 完全相等。
	y10 := func(n int64) int64 {
		return 10*tr.H0 + 10*tr.VY0*n - 49*n*(n+1)
	}
	var n int64 = 1
	for y10(n) > 0 {
		n++
	}
	prev, now := y10(n-1), y10(n) // prev>0, now<=0
	alpha := big.NewRat(prev, prev-now)
	time := new(big.Rat).Add(big.NewRat(n-1, 1), alpha)
	x := new(big.Rat).Mul(big.NewRat(tr.VX0, 1), time)
	return Landing{Frame: n, Time: time, X: x}
}

// TierLanding 用该档定点整数递推（只有加法）跑出落地事件。
func (tr Trajectory) TierLanding(t Tier) Landing {
	s := t.Scale()
	g := t.Numerator() // 每帧从 vy_raw 减去的重力分子
	x := int64(0)
	y := tr.H0 * s
	vx := tr.VX0 * s
	vy := tr.VY0 * s

	var frame int64
	var xPrev, yPrev int64
	for y > 0 {
		frame++
		xPrev, yPrev = x, y
		vy -= g
		x += vx
		y += vy
	}
	// alpha = yPrev/(yPrev-y)，原始标度在比值中消去。
	alpha := big.NewRat(yPrev, yPrev-y)
	time := new(big.Rat).Add(big.NewRat(frame-1, 1), alpha)
	xRaw := new(big.Rat).Add(
		big.NewRat(xPrev, 1),
		new(big.Rat).Mul(alpha, big.NewRat(x-xPrev, 1)),
	)
	xWorld := new(big.Rat).Quo(xRaw, big.NewRat(s, 1))
	return Landing{Frame: frame, Time: time, X: xWorld}
}

// Row 是“一条轨迹 × 一档位数”的完整对照指标。
type Row struct {
	TrajID    string `json:"traj_id"`
	TrajTitle string `json:"traj_title"`

	Tier      string  `json:"tier"`
	FracBits  int     `json:"frac_bits"`
	Numerator int64   `json:"numerator"`
	NumDigits int     `json:"num_digits"`
	Gravity   float64 `json:"gravity_approx"`

	DGravityRat   string  `json:"dgravity_rat"`
	DGravity      float64 `json:"dgravity"`
	CoeffRat      string  `json:"coeff_rat"`
	Coeff         float64 `json:"quadratic_coeff"`
	FrameBudgetN0 float64 `json:"frame_budget_continuous"`

	RefLandFrame int64   `json:"ref_land_frame"`
	RefLandTime  float64 `json:"ref_land_time"`
	RefRangeX    float64 `json:"ref_range_x"`

	// 落点差：在“参考落地整数帧 nRef”处两解的位置误差模。
	// 重力误差只作用于 y，同帧 Δx 恒为 0，故模等于 |Δy|。
	LandDiffY     float64 `json:"land_diff_y"`
	LandDiffYRat  string  `json:"land_diff_y_rat"`
	SameFrameDx   float64 `json:"same_frame_dx"`
	PassAtLanding bool    `json:"pass_at_landing"`

	// 横向偏移：比较各自插值落地点的横坐标，等于 vx·(落地时刻差)，一般非零。
	HorizontalDxRange float64 `json:"horizontal_offset_dx"`
	HorizontalDxRat   string  `json:"horizontal_offset_dx_rat"`
	LandTimeShift     float64 `json:"land_time_shift"`

	// LandingGap 是两个“插值地面落点”之间的直线距离：两点 y 都为 0，
	// 故恰等于 |HorizontalDxRange|。这是“落点差”的第二种（纯地面点）口径。
	LandingGap     float64 `json:"landing_gap"`
	LandingGapPass bool    `json:"landing_gap_within_tolerance"`

	Tolerance           float64 `json:"tolerance"`
	BreachFrame         int64   `json:"first_breach_frame"`
	BreachBeforeLanding bool    `json:"breach_before_ref_landing"`

	// 越界核对：k-1 帧误差应 <= eps，k 帧误差应 > eps。
	ErrAtKMinus1 float64 `json:"err_at_breach_minus_1"`
	ErrAtK       float64 `json:"err_at_breach"`
}

// Analyze 计算一条轨迹在一档位数下的全部指标。eps 为 nil 用默认 0.1。
func (tr Trajectory) Analyze(t Tier, eps *big.Rat) Row {
	if eps == nil {
		eps = DefaultTolerance()
	}
	ref := tr.ReferenceLanding()
	tl := tr.TierLanding(t)

	dyRat := t.ErrAtFrame(ref.Frame) // 绝对值；符号恒为“档重力更大→位置更低”
	dgF, _ := t.DGravity().Float64()
	cF, _ := t.AbsCoeff().Float64()
	dyF, _ := dyRat.Float64()
	epsF, _ := eps.Float64()
	refTimeF, _ := ref.Time.Float64()
	refXF, _ := ref.X.Float64()

	dxRangeRat := new(big.Rat).Sub(tl.X, ref.X)
	dxRangeF, _ := dxRangeRat.Float64()
	gapRat := absRat(dxRangeRat)
	gapF, _ := gapRat.Float64()
	dtRat := new(big.Rat).Sub(tl.Time, ref.Time)
	dtF, _ := dtRat.Float64()

	k := t.BreachFrame(eps)
	errKm1, _ := t.ErrAtFrame(k - 1).Float64()
	errK, _ := t.ErrAtFrame(k).Float64()

	return Row{
		TrajID:    tr.ID,
		TrajTitle: tr.Title,
		Tier:      t.Name,
		FracBits:  t.Bits,
		Numerator: t.Numerator(),
		NumDigits: t.Digits(),
		Gravity:   t.Gravity(),

		DGravityRat:   t.DGravity().RatString(),
		DGravity:      dgF,
		CoeffRat:      t.AbsCoeff().RatString(),
		Coeff:         cF,
		FrameBudgetN0: t.FrameBudgetContinuous(eps),

		RefLandFrame: ref.Frame,
		RefLandTime:  refTimeF,
		RefRangeX:    refXF,

		LandDiffY:     dyF,
		LandDiffYRat:  dyRat.RatString(),
		SameFrameDx:   0, // vx 精确且水平方向无加速度误差
		PassAtLanding: dyRat.Cmp(eps) <= 0,

		HorizontalDxRange: dxRangeF,
		HorizontalDxRat:   dxRangeRat.RatString(),
		LandTimeShift:     dtF,

		LandingGap:     gapF,
		LandingGapPass: gapRat.Cmp(eps) <= 0,

		Tolerance:           epsF,
		BreachFrame:         k,
		BreachBeforeLanding: k <= ref.Frame,

		ErrAtKMinus1: errKm1,
		ErrAtK:       errK,
	}
}

// AnalyzeAll 对两条轨迹 × 三档位数（或给定档位）逐行出指标。
func AnalyzeAll(eps *big.Rat, trajs []Trajectory, tiers []Tier) []Row {
	if trajs == nil {
		trajs = DefaultTrajectories()
	}
	if tiers == nil {
		tiers = DefaultTiers()
	}
	rows := make([]Row, 0, len(trajs)*len(tiers))
	for _, tr := range trajs {
		for _, t := range tiers {
			rows = append(rows, tr.Analyze(t, eps))
		}
	}
	return rows
}
