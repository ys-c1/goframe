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
	Bodies   int  // 实体数，默认 8
	SubSteps int  // 每帧子步数，默认 1
	Gravity  int  // 重力，Q16.16 的整数值，默认 0
}

// Command 是一帧内要执行的指令。
type Command struct {
	Tick   int // 生效帧号
	Body   int // 目标实体
	PushX  int // 冲量，Q16.16 整数表示
	PushY  int
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

// StateSnap 是一帧的可恢复快照：帧号加全部实体数值。
type StateSnap struct {
	Tick int
	Raw  []int32
}

// SaveState 保存当前帧的深拷贝快照，供回滚引擎入环。
func (w *World) SaveState() StateSnap {
	return StateSnap{Tick: w.tick, Raw: w.state.Raw()}
}

// RestoreState 恢复到指定帧快照。帧号或切片长度不匹配返回错误，
// 保证恢复后 Hash 与该帧首次推进时完全一致。
func (w *World) RestoreState(s StateSnap) error {
	if s.Tick < 0 || len(s.Raw) != 4*w.opt.Bodies {
		return fmt.Errorf("%w: tick=%d rawLen=%d", ErrBadOption, s.Tick, len(s.Raw))
	}
	w.state.Load(s.Raw)
	w.tick = s.Tick
	return nil
}

// ApplyCommands 只施加指令不推进帧号，回滚重放时逐帧使用。
func (w *World) ApplyCommands(cmds []Command) error {
	for _, c := range cmds {
		if c.Body < 0 || c.Body >= w.opt.Bodies {
			return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
		}
		w.state.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
	}
	return nil
}

// IntegrateFrame 只做一帧的子步积分并递增帧号，与 Step 的积分段完全一致。
func (w *World) IntegrateFrame() {
	for i := 0; i < w.opt.SubSteps; i++ {
		w.state.Integrate(w.opt.SubSteps)
	}
	w.tick++
}
