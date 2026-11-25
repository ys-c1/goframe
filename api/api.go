// Package api 是 goframe 的对外入口，提供确定性步进与状态哈希。
package api

import (
	"errors"
	"fmt"

	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/sim"
)

// Options 是可配参数的唯一入口，新增参数必须加在这里并注明默认值。
type Options struct {
	Bodies   int // 实体数，默认 8
	SubSteps int // 每帧子步数，默认 1
	Gravity  int // 重力，Q16.16 的整数值，默认 0
}

// Command 是一帧内要执行的指令。
type Command struct {
	Tick  int // 生效帧号
	Body  int // 目标实体
	PushX int // 冲量，Q16.16 整数表示
	PushY int
}

// World 是仿真状态。
type World struct {
	opt   Options
	state *sim.State
	tick  int
	inc   *hash.Incremental

	lastDirty int
}

var (
	ErrBadOption = errors.New("参数越界")
	ErrBadBody   = errors.New("实体编号越界")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options { return Options{Bodies: 8, SubSteps: 1, Gravity: 0} }

func (o Options) validate() error {
	if o.Bodies <= 0 {
		return fmt.Errorf("%w: Bodies=%d", ErrBadOption, o.Bodies)
	}
	if o.SubSteps <= 0 || o.SubSteps > 16 {
		return fmt.Errorf("%w: SubSteps=%d", ErrBadOption, o.SubSteps)
	}
	return nil
}

// New 按参数创建世界。
func New(opt Options) (*World, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}
	st := sim.NewState(opt.Bodies, fixed.FromInt(opt.Gravity))
	inc := hash.NewIncremental(st)
	inc.SetTick(0)
	return &World{opt: opt, state: st, inc: inc}, nil
}

// Tick 返回当前帧号。
func (w *World) Tick() int { return w.tick }

// Step 推进一帧，cmds 是本帧要执行的指令。
//
// Step 只推进仿真、记录脏槽位，不做任何哈希工作：全量用户不必为增量维护
// 付费，增量用户把维护集中到 HashIncremental 里，两种模式的计时才公平。
func (w *World) Step(cmds []Command) error {
	for _, c := range cmds {
		if c.Body < 0 || c.Body >= w.opt.Bodies {
			return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
		}
		w.state.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
	}
	for i := 0; i < w.opt.SubSteps; i++ {
		w.state.Integrate(w.opt.SubSteps)
	}
	w.tick++
	return nil
}

// Hash 返回当前状态的全量指纹：每帧从头混合全部实体。
func (w *World) Hash() uint64 { return hash.State(w.tick, w.state) }

// HashIncremental 返回增量指纹：只重算自上次调用以来变脏的槽位，并同步
// 帧号项。在满足等价条件时与 Hash 逐位相同，可用 Check 验证。
func (w *World) HashIncremental() uint64 {
	w.inc.SetTick(w.tick)
	w.lastDirty = len(w.state.Dirty())
	return w.inc.Update()
}

// Check 逐位对拍全量与增量，返回差异明细。语义上应在 HashIncremental 之后
// 调用；它本身不推进增量缓存。
func (w *World) Check() hash.Equivalence {
	w.inc.SetTick(w.tick)
	return w.inc.Check(w.tick)
}

// DirtyCount 返回最近一次 HashIncremental 真正重算的槽位数。用于测量与诊断。
func (w *World) DirtyCount() int { return w.lastDirty }

// Snapshot 返回可比较的状态切片，便于逐字节对比。
func (w *World) Snapshot() []int32 { return w.state.Raw() }
