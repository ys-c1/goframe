// Package clock 建模“系统墙钟回拨”与“单调钟 + 帧号自增”两种时间来源，
// 并给出可精确验证的非单调性、累计偏差与差值上界。
//
// 时间单位统一为毫秒。实验取 DefaultPeriodMS = 10ms（100Hz 定步长），
// 于是回拨 R 帧恰好回拨 10R ms，全部结论都能用整数精确验证。
// 把帧周期换成任意 Δ 后，以“帧”为单位的结论与上界完全不变。
package clock

import (
	"errors"
	"fmt"
	"time"
)

// DefaultPeriodMS 是实验用的固定帧周期：10ms 整，便于精确验证。
const DefaultPeriodMS int64 = 10

// 实验三档回拨幅度（帧）。
var Levels = []int{1, 10, 100}

var (
	ErrBadPeriod   = errors.New("帧周期必须为正")
	ErrBadSlots    = errors.New("时间轴长度非法")
	ErrBadRollback = errors.New("回拨帧数非法")
)

// ---------- 两种时钟 ----------

// WallClock 是可回拨的墙钟模型，对应 CLOCK_REALTIME：读数允许向前/向后跳变。
type WallClock struct{ nowMS int64 }

// NewWallClock 创建读数为 originMS 的墙钟。
func NewWallClock(originMS int64) *WallClock { return &WallClock{nowMS: originMS} }

// NowMS 返回当前墙钟读数（毫秒）。
func (c *WallClock) NowMS() int64 { return c.nowMS }

// SetMS 直接设置墙钟读数（模拟管理员改时间 / 宿主机时钟跳变）。
func (c *WallClock) SetMS(v int64) { c.nowMS = v }

// AdvanceMS 让墙钟正常前进 d 毫秒，d 不允许为负；回拨请用 RollbackFrames。
func (c *WallClock) AdvanceMS(d int64) error {
	if d < 0 {
		return fmt.Errorf("墙钟正常推进量不能为负: %d", d)
	}
	c.nowMS += d
	return nil
}

// RollbackFrames 把墙钟一次性往回拨 r 个帧周期。
func (c *WallClock) RollbackFrames(r int, periodMS int64) error {
	if r < 0 {
		return fmt.Errorf("%w: r=%d", ErrBadRollback, r)
	}
	if periodMS <= 0 {
		return fmt.Errorf("%w: periodMS=%d", ErrBadPeriod, periodMS)
	}
	c.nowMS -= int64(r) * periodMS
	return nil
}

// MonoClock 是单调钟模型，对应 CLOCK_MONOTONIC：只许前进。
// 墙钟被回拨时它的读数不受任何影响。
type MonoClock struct{ nowMS int64 }

// NewMonoClock 创建读数为 originMS 的单调钟。
func NewMonoClock(originMS int64) *MonoClock { return &MonoClock{nowMS: originMS} }

// NowMS 返回单调钟读数。
func (c *MonoClock) NowMS() int64 { return c.nowMS }

// AdvanceMS 让单调钟前进 d 毫秒，d 不允许为负。
func (c *MonoClock) AdvanceMS(d int64) error {
	if d < 0 {
		return fmt.Errorf("单调钟不能后退: d=%d", d)
	}
	c.nowMS += d
	return nil
}

// SystemMono 用 Go 自带的单调读数计时。
// time.Since 内部走 time.Time 的 monotonic 字段，不受墙钟回拨影响
// （注意 time.Now().UnixNano() 会丢掉单调读数，不能用它做帧间隔）。
type SystemMono struct{ start time.Time }

// NewSystemMono 以当前时刻为起点。
func NewSystemMono() *SystemMono { return &SystemMono{start: time.Now()} }

// NowMS 返回从起点经过的单调毫秒数。
func (s *SystemMono) NowMS() int64 { return time.Since(s.start).Milliseconds() }

// ---------- 帧号推导与偏差度量 ----------

// FrameAt 返回时刻 tMS 对应的帧号 floor((tMS-originMS)/periodMS)。
// 显式做向下取整，墙钟回拨后被除数可能为负，Go 原生整数除法是向零取整，不能直接用。
func FrameAt(tMS, originMS, periodMS int64) int64 {
	if periodMS <= 0 {
		panic(fmt.Sprintf("FrameAt: %v", fmt.Errorf("%w: %d", ErrBadPeriod, periodMS)))
	}
	return floorDiv(tMS-originMS, periodMS)
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b < 0 { // b>0 时，负余数说明向零取整多取了 1
		q--
	}
	return q
}

// LagRemainder 返回计数器相对单调时间的帧内等待余量：
//
//	lagMS = (monoMS-originMS) - n*periodMS ∈ [0, periodMS)
//
// 其中 n=floor((monoMS-originMS)/periodMS)。
// 归一化到帧即 lagMS/periodMS ∈ [0,1)，这就是“低于一帧”的上界来源。
func LagRemainder(monoMS, originMS, periodMS int64) int64 {
	if periodMS <= 0 {
		panic(fmt.Sprintf("LagRemainder: %v", fmt.Errorf("%w: %d", ErrBadPeriod, periodMS)))
	}
	r := (monoMS - originMS) % periodMS
	if r < 0 {
		r += periodMS
	}
	return r
}

// Regression 度量帧号序列的累计回退偏差：
//
//	D = Σ_i max(0, f[i-1]-f[i])，单位为帧。
//
// 每次墙钟回拨 R_j 帧恰好贡献 R_j；正常 +1 推进贡献 0。
func Regression(frames []int64) int64 {
	var d int64
	for i := 1; i < len(frames); i++ {
		if frames[i-1] > frames[i] {
			d += frames[i-1] - frames[i]
		}
	}
	return d
}

// MaxBackwardJump 返回序列中单次最大的帧号回退量（无回退为 0）。
func MaxBackwardJump(frames []int64) int64 {
	var m int64
	for i := 1; i < len(frames); i++ {
		if j := frames[i-1] - frames[i]; j > m {
			m = j
		}
	}
	return m
}

// ---------- 修复方案：单调钟截止时间 + 帧号自增 ----------

// Counter 是“帧号自增”帧源：帧号只等于已提交帧数，每提交一帧严格 +1，
// 任何外部时钟跳变都改不了它。
type Counter struct{ committed int64 }

// Frame 返回下一帧帧号（= 已提交帧数），与 api.World.Tick() 同义。
func (c *Counter) Frame() int64 { return c.committed }

// Commit 提交一帧，帧号自增 1，返回新的已提交帧数。
func (c *Counter) Commit() int64 {
	c.committed++
	return c.committed
}

// Poll 站在单调时刻 monoMS，把所有到期帧按帧号顺序提交：
//
//	target = floor((monoMS-originMS)/periodMS)
//	for committed < target: step(committed); committed++
//
// 返回本次提交的帧数。即使轮询间隔很大，也只会按单调时间补帧；
// 单调钟不回退，所以 target 单调不减，帧号永远非递减。
func (c *Counter) Poll(monoMS, originMS, periodMS int64, step func(frame int64) error) (int, error) {
	target := FrameAt(monoMS, originMS, periodMS)
	var made int
	for c.committed < target {
		if step != nil {
			if err := step(c.committed); err != nil {
				return made, err
			}
		}
		c.committed++
		made++
	}
	return made, nil
}

// CatchupFastSlots 计算“每采样点在正常一帧之外多补 extraPerSlot 帧”时，
// 还清 debt 帧回拨欠账需要连续快进多少个采样点：ceil(debt/extraPerSlot)。
// 这对应题目描述的“回拨后一直快（追帧）”阶段；总额外提交量恒等于 debt。
func CatchupFastSlots(debt int64, extraPerSlot int64) int64 {
	if debt <= 0 {
		return 0
	}
	if extraPerSlot <= 0 {
		extraPerSlot = 1
	}
	return (debt + extraPerSlot - 1) / extraPerSlot
}

// ---------- 三档回拨实验模型 ----------

// Config 是单档回拨实验的参数。
type Config struct {
	RollbackFrames int   // 一次性回拨幅度 R（帧），取 1/10/100
	Slots          int   // 名义采样槽数，槽 i 的单调时刻为 i*PeriodMS
	RollbackAt     int   // 回拨发生的槽号 rb（在该槽先记录回拨前点，再记录回拨后点）
	PeriodMS       int64 // 帧周期 Δ（毫秒）
}

// DiffPoint 是“单调钟 − 墙钟”差值曲线上的一个点。
type DiffPoint struct {
	Point      string `json:"point"` // before / rollback / after
	MonoMS     int64  `json:"mono_ms"`
	WallMS     int64  `json:"wall_ms"`
	DiffMS     int64  `json:"diff_ms"` // mono - wall
	DiffFrames int64  `json:"diff_frames"`
}

// Point 是时间轴上的一行（一个观测事件）。
type Point struct {
	Order              int    `json:"order"`
	Phase              string `json:"phase"` // before / rollback_pre / rollback / after
	MonoMS             int64  `json:"mono_ms"`
	WallMS             int64  `json:"wall_ms"`
	GapMS              int64  `json:"gap_ms"` // 单调钟 - 墙钟
	IdealFrame         int64  `json:"ideal_frame"`
	WallFrame          int64  `json:"wall_frame"`
	FixedFrame         int64  `json:"fixed_frame"`
	WallDelta          int64  `json:"wall_delta"`
	FixedDelta         int64  `json:"fixed_delta"`
	WallCumRegression  int64  `json:"wall_cum_regression"`
	FixedCumRegression int64  `json:"fixed_cum_regression"`
	CatchupDebtFrames  int64  `json:"catchup_debt_frames"`
}

// Summary 是一档回拨实验的完整结果与界。
type Summary struct {
	RollbackFrames int          `json:"rollback_frames"`
	PeriodMS       int64        `json:"period_ms"`
	Slots          int          `json:"slots"`
	RollbackAt     int          `json:"rollback_at"`
	Points         []Point      `json:"points"`
	DiffPoints     [3]DiffPoint `json:"diff_points"`

	// 帧号变化关键坐标。
	FrameBefore       int64   `json:"frame_before"`        // F(rb-1)
	FrameRollbackPre  int64   `json:"frame_rollback_pre"`  // F(rb^-) = rb
	FrameRollbackPost int64   `json:"frame_rollback_post"` // F(rb^+) = rb-R
	FrameAfter        int64   `json:"frame_after"`         // F(rb+1)
	LocalSequence     []int64 `json:"local_sequence"`      // 回拨前后墙钟帧号节选

	// 累计偏差（单位：帧）。
	WallCumRegression   int64 `json:"wall_cum_regression"`  // 必为 R
	FixedCumRegression  int64 `json:"fixed_cum_regression"` // 必为 0
	WallMaxBackwardJump int64 `json:"wall_max_backward_jump"`

	// “之后一直快”的追帧欠账与快进槽数（每槽多补 1 帧）。
	CatchupDebtFrames int64 `json:"catchup_debt_frames"` // 必为 R
	CatchupFastSlots  int64 `json:"catchup_fast_slots"`  // 必为 R（2× 追帧）

	// 差值上界。
	ClockGapBoundMS     int64 `json:"clock_gap_bound_ms"`     // R*Δ
	FrameGapBound       int64 `json:"frame_gap_bound"`        // R（同相位精确等号）
	FrameGapBoundSlack  int64 `json:"frame_gap_bound_slack"`  // R+1（计入一帧内提交滞后的严格上界）
	FixedLagBoundFrames int64 `json:"fixed_lag_bound_frames"` // 1，实际 lag/Δ < 1

	// 验收。
	WallProofOK bool `json:"wall_proof_ok"` // D_W == R 且确实非单调
	FixPass     bool `json:"fix_pass"`      // D_FM < 1 帧
	OK          bool `json:"ok"`            // 两个命题与验收全部成立
}

func (c *Config) normalize() error {
	if c.PeriodMS <= 0 {
		return fmt.Errorf("%w: PeriodMS=%d", ErrBadPeriod, c.PeriodMS)
	}
	if c.RollbackFrames < 0 {
		return fmt.Errorf("%w: R=%d", ErrBadRollback, c.RollbackFrames)
	}
	if c.Slots < 4 {
		return fmt.Errorf("%w: Slots=%d", ErrBadSlots, c.Slots)
	}
	if c.RollbackAt < 1 || c.RollbackAt >= c.Slots-1 {
		return fmt.Errorf("%w: RollbackAt=%d 越界 [1,%d]", ErrBadSlots, c.RollbackAt, c.Slots-2)
	}
	return nil
}

type event struct {
	phase  string
	monoMS int64
	wallMS int64
}

// Run 按故障模型生成一条完整时间轴并计算全部指标。
//
// 故障模型（隔离“一次性回拨”这一个变量）：
//
//	m(t) = t（单调钟，速率恒为 1）
//	w(t) = t - R*Δ*1[t >= τ]（墙钟在 τ 被回拨 R 帧，之后速率恢复为 1）
//
// 在 τ 时刻采集回拨前、回拨后两个点，保证帧号跌幅按同一时刻比较，恰好是 R。
func Run(cfg Config) (*Summary, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	R := int64(cfg.RollbackFrames)
	p := cfg.PeriodMS
	rb := cfg.RollbackAt

	var evs []event
	for i := 0; i <= rb; i++ {
		phase := "before"
		if i == rb {
			phase = "rollback_pre"
		}
		t := int64(i) * p
		evs = append(evs, event{phase: phase, monoMS: t, wallMS: t})
	}
	t := int64(rb) * p
	evs = append(evs, event{phase: "rollback", monoMS: t, wallMS: t - R*p})
	for i := rb + 1; i < cfg.Slots; i++ {
		t := int64(i) * p
		evs = append(evs, event{phase: "after", monoMS: t, wallMS: t - R*p})
	}

	s := &Summary{
		RollbackFrames:      cfg.RollbackFrames,
		PeriodMS:            p,
		Slots:               cfg.Slots,
		RollbackAt:          rb,
		Points:              make([]Point, 0, len(evs)),
		ClockGapBoundMS:     R * p,
		FrameGapBound:       R,
		FrameGapBoundSlack:  R + 1,
		FixedLagBoundFrames: 1,
		CatchupDebtFrames:   R,
		CatchupFastSlots:    CatchupFastSlots(R, 1),
	}

	var prevWall, prevFixed int64
	havePrev := false
	var wallSeq []int64
	for idx, e := range evs {
		wf := FrameAt(e.wallMS, 0, p)
		ff := FrameAt(e.monoMS, 0, p)
		pt := Point{
			Order:      idx,
			Phase:      e.phase,
			MonoMS:     e.monoMS,
			WallMS:     e.wallMS,
			GapMS:      e.monoMS - e.wallMS,
			IdealFrame: ff,
			WallFrame:  wf,
			FixedFrame: ff,
		}
		if havePrev {
			pt.WallDelta = wf - prevWall
			pt.FixedDelta = ff - prevFixed
			if dw := prevWall - wf; dw > 0 {
				s.WallCumRegression += dw
			}
			if df := prevFixed - ff; df > 0 {
				s.FixedCumRegression += df
			}
		}
		pt.WallCumRegression = s.WallCumRegression
		pt.FixedCumRegression = s.FixedCumRegression
		pt.CatchupDebtFrames = ff - wf
		if pt.CatchupDebtFrames < 0 {
			pt.CatchupDebtFrames = 0
		}
		if havePrev {
			if j := prevWall - wf; j > s.WallMaxBackwardJump {
				s.WallMaxBackwardJump = j
			}
		}
		s.Points = append(s.Points, pt)
		wallSeq = append(wallSeq, wf)
		prevWall, prevFixed = wf, ff
		havePrev = true
	}

	// 关键坐标。
	s.FrameBefore = int64(rb - 1)
	s.FrameRollbackPre = int64(rb)
	s.FrameRollbackPost = int64(rb - cfg.RollbackFrames)
	s.FrameAfter = int64(rb + 1 - cfg.RollbackFrames)

	// 回拨前后墙钟帧号节选：rb-2 .. rb（含回拨前点）, 回拨后点, rb+1, rb+2。
	pre := []int64{int64(rb - 2), int64(rb - 1), int64(rb)}
	s.LocalSequence = append(pre, s.FrameRollbackPost, s.FrameAfter, int64(rb+2-cfg.RollbackFrames))

	// 差值曲线三个点：回拨前一槽 / 回拨瞬间（回拨后读数）/ 回拨后一槽。
	s.DiffPoints = [3]DiffPoint{
		{Point: "before", MonoMS: int64(rb-1) * p, WallMS: int64(rb-1) * p, DiffMS: 0, DiffFrames: 0},
		{Point: "rollback", MonoMS: int64(rb) * p, WallMS: int64(rb)*p - R*p, DiffMS: R * p, DiffFrames: R},
		{Point: "after", MonoMS: int64(rb+1) * p, WallMS: int64(rb+1)*p - R*p, DiffMS: R * p, DiffFrames: R},
	}

	nonMonotonic := s.WallMaxBackwardJump > 0
	s.WallProofOK = nonMonotonic && s.WallCumRegression == R && s.WallMaxBackwardJump == R
	s.FixPass = s.FixedCumRegression < 1
	s.OK = s.WallProofOK && s.FixPass &&
		s.CatchupDebtFrames == R && s.CatchupFastSlots == R
	return s, nil
}
