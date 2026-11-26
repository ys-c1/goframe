// Package api 是 goframe 的对外入口，提供确定性步进与状态哈希。
package api

import (
	"errors"
	"fmt"

	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/rollback"
	"goframe/internal/sim"
	"goframe/internal/snap"
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
	opt     Options
	state   *sim.State
	tick    int
	index   *snap.Index
	history *rollback.Ring // 五帧回滚环，只存“编号+旧值”差分
}

var (
	ErrBadOption       = errors.New("参数越界")
	ErrBadBody         = errors.New("实体编号越界")
	ErrNoRollback      = errors.New("没有可回滚的帧")
	ErrRollbackCorrupt = errors.New("回滚差分损坏")
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
	// validate 已保证 Bodies>0，编号表不会失败。
	idx, err := snap.NewIndex(opt.Bodies)
	if err != nil {
		return nil, err
	}
	return &World{
		opt:     opt,
		state:   sim.NewState(opt.Bodies, fixed.FromInt(opt.Gravity)),
		index:   idx,
		history: rollback.NewFive(),
	}, nil
}

// Tick 返回当前帧号。
func (w *World) Tick() int { return w.tick }

// Step 推进一帧，cmds 是本帧要执行的指令。
// 成功推进后，自动把本帧的回滚差分（被改字段编号 + 帧前旧值）压入五帧环。
func (w *World) Step(cmds []Command) error {
	tickBefore := w.tick
	before := w.state.Raw() // Raw 返回新切片，后续改写不会污染它
	for _, c := range cmds {
		if c.Body < 0 || c.Body >= w.opt.Bodies {
			return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
		}
		w.state.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
	}
	for i := 0; i < w.opt.SubSteps; i++ {
		w.state.Integrate(w.opt.SubSteps)
	}

	after := w.state.Raw()
	d, err := snap.Build(w.index, before, after)
	if err != nil {
		return fmt.Errorf("构建回滚差分失败: %w", err)
	}
	d.TickBefore = tickBefore
	codec, err := d.MarshalBinary()
	if err != nil {
		return fmt.Errorf("编码回滚差分失败: %w", err)
	}
	w.history.Push(rollback.Blob{TickBefore: tickBefore, Codec: codec})

	w.tick++
	return nil
}

// Hash 返回当前状态的指纹。
func (w *World) Hash() uint64 { return hash.State(w.tick, w.state) }

// Snapshot 返回可比较的状态切片，便于逐字节对比。
func (w *World) Snapshot() []int32 { return w.state.Raw() }

// FieldIndex 返回字段编号表，编号与 Snapshot() 的下标一一对应。
func (w *World) FieldIndex() *snap.Index { return w.index }

// RollbackFrames 返回五帧环中当前可回滚的帧数。
func (w *World) RollbackFrames() int { return w.history.Len() }

// CanRollback 报告是否至少有一帧可回滚。
func (w *World) CanRollback() bool { return w.history.Len() > 0 }

// Rollback 弹出最新一帧差分并逐字段写回旧值，把世界恢复到该帧 Step 之前。
// 还原正确性由 snap 包的逐字段比对测试与 cmd/snapcheck 的三档对照保证。
func (w *World) Rollback() error {
	b, ok := w.history.Pop()
	if !ok {
		return ErrNoRollback
	}
	d, err := snap.UnmarshalDelta(b.Codec)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRollbackCorrupt, err)
	}
	if d.N != w.index.N() || b.TickBefore != d.TickBefore {
		return fmt.Errorf("%w: 帧元数据不一致", ErrRollbackCorrupt)
	}
	// 只写回差分条目里的字段；未列出的字段本帧未变，无需触碰。
	for _, e := range d.Entries {
		if err := w.state.SetScalar(int(e.ID), e.Old); err != nil {
			return fmt.Errorf("%w: %v", ErrRollbackCorrupt, err)
		}
	}
	w.tick = b.TickBefore
	return nil
}
