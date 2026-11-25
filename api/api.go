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
	return &World{opt: opt, state: sim.NewState(opt.Bodies, fixed.FromInt(opt.Gravity))}, nil
}

// Tick 返回当前帧号。
func (w *World) Tick() int { return w.tick }

// Step 推进一帧，cmds 是本帧要执行的指令。
func (w *World) Step(cmds []Command) error {
	for _, c := range cmds {
		if c.Body < 0 || c.Body >= w.opt.Bodies {
			return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
		}
		w.state.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
	}
	// 每帧跑 opt.SubSteps 个子步；Integrate 内部按子步相位对重力整除余数
	// 做补偿，使这 SubSteps 次施加的重力冲量之和逐帧严格等于设定值。
	for i := 0; i < w.opt.SubSteps; i++ {
		w.state.Integrate(w.opt.SubSteps)
	}
	w.tick++
	return nil
}

// Hash 返回当前状态的指纹。
func (w *World) Hash() uint64 { return hash.State(w.tick, w.state) }

// Snapshot 返回可比较的状态切片，便于逐字节对比。
func (w *World) Snapshot() []int32 { return w.state.Raw() }

// FrameGravityImpulse 返回最近一帧每个实体实际收到的重力冲量，
// 单位为 Q16.16 raw（设定值 = int64(opt.Gravity)<<16）。
func (w *World) FrameGravityImpulse() int64 { return w.state.LastFrameGravityImpulse() }

// TotalGravityImpulse 返回截至当前帧每个实体累计收到的重力冲量（raw）。
func (w *World) TotalGravityImpulse() int64 { return w.state.TotalGravityImpulse() }
