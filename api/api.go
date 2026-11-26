// Package api 是 goframe 的对外入口，提供确定性步进与状态哈希。
package api

import (
	"errors"
	"fmt"

	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/sim"
)

// 指令类型。
const (
	OpPush    = 0 // 对 Body 施加冲量
	OpSpawn   = 1 // 生成实体，返回的编号写入 Command.ID
	OpDespawn = 2 // 销毁 Body
)

// SpawnUnallocated 是 OpSpawn 在记录侧的占位编号；
// Step 分配到单调编号后回填，回放侧携带的编号必须与之严格相等。
const SpawnUnallocated = -1

// Options 是可配参数的唯一入口，新增参数必须加在这里并注明默认值。
type Options struct {
	Bodies        int // 初始实体数，默认 8
	SubSteps      int // 每帧子步数，默认 1
	Gravity       int // 重力，Q16.16 的整数值，默认 0
	SpawnsPerTick int // 每帧至多生成实体数，默认 64（与 sim.MaxID 的时长推导一致）
}

// Command 是一帧内要执行的指令。
type Command struct {
	Tick  int // 生效帧号
	Op    int // 指令类型，默认 OpPush
	Body  int // 目标实体编号（OpPush/OpDespawn）
	ID    int // OpSpawn 分配到的单调编号（记录时回填，回放时按序校验）
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
	ErrBadOption   = errors.New("参数越界")
	ErrBadBody     = errors.New("实体编号越界或已销毁")
	ErrBadCommand  = errors.New("非法指令")
	ErrIDExhausted = sim.ErrIDExhausted
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options {
	return Options{Bodies: 8, SubSteps: 1, Gravity: 0, SpawnsPerTick: 64}
}

func (o Options) validate() error {
	if o.Bodies < 0 {
		return fmt.Errorf("%w: Bodies=%d", ErrBadOption, o.Bodies)
	}
	if o.SubSteps <= 0 || o.SubSteps > 16 {
		return fmt.Errorf("%w: SubSteps=%d", ErrBadOption, o.SubSteps)
	}
	if o.SpawnsPerTick <= 0 || o.SpawnsPerTick > 64 {
		return fmt.Errorf("%w: SpawnsPerTick=%d", ErrBadOption, o.SpawnsPerTick)
	}
	return nil
}

// New 按参数创建世界。
func New(opt Options) (*World, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}
	st, err := sim.NewStateWithLimit(opt.Bodies, fixed.FromInt(opt.Gravity), sim.MaxID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadOption, err)
	}
	return &World{opt: opt, state: st}, nil
}

// Tick 返回当前帧号。
func (w *World) Tick() int { return w.tick }

// State 暴露内部状态，供实验/工具包使用。
func (w *World) State() *sim.State { return w.state }

// Step 推进一帧，cmds 是本帧要执行的指令。
// 指令按序执行；OpSpawn 分配的编号单调递增、永不复用。
func (w *World) Step(cmds []Command) error {
	spawns := 0
	for i := range cmds {
		c := &cmds[i]
		switch c.Op {
		case OpPush:
			if !w.state.Alive(c.Body) {
				return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
			}
			w.state.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
		case OpDespawn:
			if !w.state.Alive(c.Body) {
				return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
			}
			w.state.Despawn(c.Body)
		case OpSpawn:
			spawns++
			if spawns > w.opt.SpawnsPerTick {
				return fmt.Errorf("%w: 单帧生成数超过 SpawnsPerTick=%d", ErrBadOption, w.opt.SpawnsPerTick)
			}
			b, err := w.state.Spawn()
			if err != nil {
				return err
			}
			// 回放时日志里记录的编号必须与当前分配严格一致，
			// 否则说明两端的增删序列已经分叉。
			if c.ID != SpawnUnallocated && c.ID != b.ID {
				return fmt.Errorf("%w: 回放编号不一致 log=%d alloc=%d", ErrBadCommand, c.ID, b.ID)
			}
			c.ID = b.ID
		default:
			return fmt.Errorf("%w: Op=%d", ErrBadCommand, c.Op)
		}
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
