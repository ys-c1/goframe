// Package clock 用累加余数法（Bresenham 思路）在整数毫秒帧长上逼近目标帧率。
//
// 理想帧长 1000/r 往往不是整数。本包把每帧取整产生的余数累积起来，按余数
// 在“短帧 q 毫秒”和“长帧 q+1 毫秒”之间分配：全程只做整数运算，任何一帧
// 的帧长都能由帧号经纯函数 FrameAt 独立复算，适合确定性帧同步与回放对拍。
package clock

import (
	"errors"
	"fmt"
)

// 毫秒与秒的换算，单独命名以便公式对照。
const msPerSecond = 1000

var (
	// ErrBadRate 表示目标帧率越界。
	ErrBadRate = errors.New("帧率越界")
	// ErrBadFrames 表示帧数或帧号越界。
	ErrBadFrames = errors.New("帧数越界")
	// ErrBadFrameMS 表示固定帧长越界。
	ErrBadFrameMS = errors.New("帧长越界")
)

// Scheduler 是无浮点的长短帧分配器，零值不可用，用 New 构造。
type Scheduler struct {
	rate int // 目标帧率 Hz
	q    int // 短帧长，毫秒
	p, d int // 余数部分约分 p/d = (1000 mod rate)/rate
	rem  int // 余数寄存器，单位为 1/rate 毫秒
	tick int // 已产出的帧数
}

// New 按目标帧率构造分配器，帧率须落在 [1,1000]。
func New(rateHz int) (*Scheduler, error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return nil, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	p, d := reduced(msPerSecond%rateHz, rateHz)
	return &Scheduler{rate: rateHz, q: msPerSecond / rateHz, p: p, d: d}, nil
}

// RateHz 返回目标帧率。
func (s *Scheduler) RateHz() int { return s.rate }

// ShortMS 返回短帧长（毫秒）。
func (s *Scheduler) ShortMS() int { return s.q }

// LongMS 返回长帧长（毫秒）；帧率整除 1000 时没有长短之分，与短帧长相同。
func (s *Scheduler) LongMS() int {
	if s.p == 0 {
		return s.q
	}
	return s.q + 1
}

// Tick 返回已产出的帧数。
func (s *Scheduler) Tick() int { return s.tick }

// Remainder 返回余数寄存器当前值，单位为 1/rate 毫秒，仅用于检视与复算。
func (s *Scheduler) Remainder() int { return s.rem }

// Next 产出下一帧的帧长（毫秒）并推进帧号。
//
// 每帧累加 (1000 mod rate) 个 1/rate 毫秒的余数；余数凑够一个整毫秒
// （达到 rate）就本帧发长帧并扣回 rate，否则发短帧。
func (s *Scheduler) Next() int {
	s.rem += msPerSecond % s.rate
	ms := s.q
	if s.rem >= s.rate {
		s.rem -= s.rate
		ms++
	}
	s.tick++
	return ms
}

// FrameAt 是纯函数：返回第 idx 帧（从 0 起）的帧长，不依赖任何运行状态。
// 它与同帧率 Scheduler 逐帧 Next 的结果逐帧相等，可用于离线复算任意一帧。
//
// 设 q = ⌊1000/r⌋，余数部分约分为 p/d，则
//
//	FrameAt(r, idx) = q + ⌊(idx+1)p/d⌋ − ⌊idx·p/d⌋
//
// 因 0 ≤ p < d，差值只可能是 0（短帧）或 1（长帧）。
func FrameAt(rateHz, idx int) (int, error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return 0, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	if idx < 0 {
		return 0, fmt.Errorf("%w: idx=%d", ErrBadFrames, idx)
	}
	q := msPerSecond / rateHz
	p, d := reduced(msPerSecond%rateHz, rateHz)
	long := (int64(idx+1)*int64(p))/int64(d) - (int64(idx)*int64(p))/int64(d)
	return q + int(long), nil
}

// Distribution 返回前 n 帧中长帧与短帧的数量，long+short 恒等于 n。
func Distribution(rateHz, n int) (long, short int, err error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return 0, 0, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	if n < 0 {
		return 0, 0, fmt.Errorf("%w: n=%d", ErrBadFrames, n)
	}
	p, d := reduced(msPerSecond%rateHz, rateHz)
	long = int(int64(n) * int64(p) / int64(d))
	return long, n - long, nil
}

// LongShortRatio 返回一个完整分配周期内的长帧数与短帧数（长:短 = p : d−p），
// 周期长度为 d 帧；帧率整除 1000 时返回 (0,1)，即全部等长。
func LongShortRatio(rateHz int) (longPerCycle, shortPerCycle int, err error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return 0, 0, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	p, d := reduced(msPerSecond%rateHz, rateHz)
	return p, d - p, nil
}

// CycleLen 返回长短帧分配周期的帧数，即约分后的分母 d。
func CycleLen(rateHz int) (int, error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return 0, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	_, d := reduced(msPerSecond%rateHz, rateHz)
	return d, nil
}

// DriftMS 返回前 n 帧结束时的累计偏差（已调度时间减理想时间），
// 以精确分数 num/den 毫秒表示，分母恒正，且恒有 −1 < num/den ≤ 0。
// n=0 或 n 为周期整数倍时偏差为 0。
func DriftMS(rateHz, n int) (num, den int, err error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return 0, 0, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	if n < 0 {
		return 0, 0, fmt.Errorf("%w: n=%d", ErrBadFrames, n)
	}
	p, d := reduced(msPerSecond%rateHz, rateHz)
	num = -int((int64(n) * int64(p)) % int64(d))
	if num == 0 {
		return 0, 1, nil
	}
	return num, d, nil
}

// MaxAbsDriftMS 返回前 n 帧过程中每个前缀末尾绝对偏差的最大值，
// 以分数 num/den 毫秒表示。偏差为负或零，这里取其绝对值的上确界。
func MaxAbsDriftMS(rateHz, n int) (num, den int, err error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return 0, 0, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	if n < 0 {
		return 0, 0, fmt.Errorf("%w: n=%d", ErrBadFrames, n)
	}
	p, d := reduced(msPerSecond%rateHz, rateHz)
	best := 0
	for k := 1; k <= n; k++ {
		r := int((int64(k) * int64(p)) % int64(d))
		if r > best {
			best = r
		}
	}
	return best, d, nil
}

// FixedDriftMS 返回“恒定帧长 fixedMS 跑 n 帧”这一朴素方案的累计偏差，
// 以精确分数 num/den 毫秒表示，即 n·(fixedMS − 1000/rate)，随 n 线性变化。
func FixedDriftMS(rateHz, fixedMS, n int) (num, den int, err error) {
	if rateHz <= 0 || rateHz > msPerSecond {
		return 0, 0, fmt.Errorf("%w: rate=%d", ErrBadRate, rateHz)
	}
	if fixedMS < 0 {
		return 0, 0, fmt.Errorf("%w: fixedMS=%d", ErrBadFrameMS, fixedMS)
	}
	if n < 0 {
		return 0, 0, fmt.Errorf("%w: n=%d", ErrBadFrames, n)
	}
	num = n * (fixedMS*rateHz - msPerSecond)
	den = rateHz
	if num == 0 {
		return 0, 1, nil
	}
	g := gcd(abs(num), den)
	return num / g, den / g, nil
}

// reduced 把 num/den 约分为最简分数；num 为 0 时约定分母为 1。
func reduced(num, den int) (p, d int) {
	if num == 0 {
		return 0, 1
	}
	g := gcd(num, den)
	return num / g, den / g
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
