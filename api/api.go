// Package api 是 goframe 的对外入口，提供确定性步进与状态哈希。
package api

import (
	"errors"
	"fmt"

	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/rng"
	"goframe/internal/sim"
)

// Options 是可配参数的唯一入口，新增参数必须加在这里并注明默认值。
type Options struct {
	Bodies   int // 实体数，默认 8
	SubSteps int // 每帧子步数，默认 1
	Gravity  int // 重力，Q16.16 的整数值，默认 0

	// Seed 是随机扰动的主种子，必须固定并随 replay.Log.Options 一起记录，
	// 否则回放无法复现扰动。默认 0（仅在 PerturbAmp>0 时相关）。
	Seed int64
	// PerturbAmp 是每帧施加给每个实体的随机速度扰动幅度，Q16.16 原始单位，
	// 扰动落在 [-PerturbAmp, PerturbAmp]。默认 0 表示关闭，状态与旧版逐字节一致。
	// 扰动按 (Seed, 实体编号, 轴, 帧号) 派生，与调用次序无关。
	PerturbAmp int
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
	if o.PerturbAmp < 0 {
		return fmt.Errorf("%w: PerturbAmp=%d", ErrBadOption, o.PerturbAmp)
	}
	if o.PerturbAmp > 1<<30 {
		return fmt.Errorf("%w: PerturbAmp=%d 过大", ErrBadOption, o.PerturbAmp)
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
	if w.opt.PerturbAmp > 0 {
		amp := int32(w.opt.PerturbAmp)
		seed := uint64(w.opt.Seed)
		tick := uint64(w.tick)
		for i := 0; i < w.opt.Bodies; i++ {
			// 每个实体、每轴、每帧独立派生， draws 之间没有共享游标，
			// 因此新增/跳过任何实体的调用都不会平移其他实体。
			jx := rng.Jitter(seed, i, rng.TagJitterX, tick, amp)
			jy := rng.Jitter(seed, i, rng.TagJitterY, tick, amp)
			w.state.Push(i, jx, jy)
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
