// Package api 是 goframe 的对外入口，提供确定性步进与状态哈希。
package api

import (
	"errors"
	"fmt"

	"goframe/internal/fixed"
	"goframe/internal/hash"
	"goframe/internal/queue"
	"goframe/internal/sim"
)

// Options 是可配参数的唯一入口，新增参数必须加在这里并注明默认值。
type Options struct {
	Bodies    int // 实体数，默认 8
	SubSteps  int // 每帧子步数，默认 1
	Gravity   int // 重力，Q16.16 的整数值，默认 0
	QueueSize int // 每帧指令队列容量，默认 0 表示无界（不丢弃任何指令）
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
	q       *queue.Queue
	unbound bool // QueueSize<=0 时不经过队列丢弃逻辑
}

var (
	ErrBadOption = errors.New("参数越界")
	ErrBadBody   = errors.New("实体编号越界")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options {
	return Options{Bodies: 8, SubSteps: 1, Gravity: 0, QueueSize: 0}
}

func (o Options) validate() error {
	if o.Bodies <= 0 {
		return fmt.Errorf("%w: Bodies=%d", ErrBadOption, o.Bodies)
	}
	if o.SubSteps <= 0 || o.SubSteps > 16 {
		return fmt.Errorf("%w: SubSteps=%d", ErrBadOption, o.SubSteps)
	}
	if o.QueueSize < 0 {
		return fmt.Errorf("%w: QueueSize=%d", ErrBadOption, o.QueueSize)
	}
	return nil
}

// New 按参数创建世界。QueueSize > 0 时使用有界队列：
// 入队即决策（Immediate）并按冲量大小保留最大的 QueueSize 条（TopK）。
func New(opt Options) (*World, error) {
	return NewWithQueue(opt, queue.Immediate, queue.TopK)
}

// NewWithQueue 按指定丢弃时机与保留策略创建世界。
// opt.QueueSize <= 0 时队列无界，timing 与 policy 不产生作用。
func NewWithQueue(opt Options, timing queue.Timing, policy queue.Policy) (*World, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}
	return &World{
		opt:     opt,
		state:   sim.NewState(opt.Bodies, fixed.FromInt(opt.Gravity)),
		q:       queue.New(timing, policy),
		unbound: opt.QueueSize <= 0,
	}, nil
}

// Tick 返回当前帧号。
func (w *World) Tick() int { return w.tick }

// impulseWeight 返回指令冲量的 L1 大小 |PushX|+|PushY|，即丢弃分析中的权重。
func impulseWeight(c Command) int64 {
	x, y := int64(c.PushX), int64(c.PushY)
	if x < 0 {
		x = -x
	}
	if y < 0 {
		y = -y
	}
	return x + y
}

// Step 推进一帧，cmds 是本帧到达的指令。
// 队列有界时，超出容量的指令按构造时指定的时机与策略被丢弃，
// 只有保留下来的指令才会真正施加到状态上。
func (w *World) Step(cmds []Command) error {
	for _, c := range cmds {
		if c.Body < 0 || c.Body >= w.opt.Bodies {
			return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
		}
	}

	kept := cmds
	if !w.unbound {
		cap := w.opt.QueueSize
		if cap > len(cmds) {
			cap = len(cmds)
		}
		w.q.BeginFrame(cap)
		for i, c := range cmds {
			w.q.Enqueue(i, impulseWeight(c))
		}
		idx := w.q.Drain(cap)
		kept = make([]Command, len(idx))
		for j, i := range idx {
			kept[j] = cmds[i]
		}
	}

	for _, c := range kept {
		w.state.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
	}
	for i := 0; i < w.opt.SubSteps; i++ {
		w.state.Integrate(w.opt.SubSteps)
	}
	w.tick++
	return nil
}

// StepWithCap 以本帧动态容量 cap 推进一帧，始终经过队列丢弃逻辑，
// 用于按阈值 τ 逐帧计算 K(τ) 的保留策略。cap >= len(cmds) 时本帧不丢弃。
func (w *World) StepWithCap(cmds []Command, cap int) error {
	for _, c := range cmds {
		if c.Body < 0 || c.Body >= w.opt.Bodies {
			return fmt.Errorf("%w: Body=%d", ErrBadBody, c.Body)
		}
	}
	if cap < 0 {
		cap = 0
	}
	if cap > len(cmds) {
		cap = len(cmds)
	}
	w.q.BeginFrame(cap)
	for i, c := range cmds {
		w.q.Enqueue(i, impulseWeight(c))
	}
	idx := w.q.Drain(cap)
	kept := make([]Command, len(idx))
	for j, i := range idx {
		kept[j] = cmds[i]
	}
	for _, c := range kept {
		w.state.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
	}
	for i := 0; i < w.opt.SubSteps; i++ {
		w.state.Integrate(w.opt.SubSteps)
	}
	w.tick++
	return nil
}

// DropCount 返回截至当前累计丢弃的指令数。
func (w *World) DropCount() int { return w.q.DropCount() }

// DropWeight 返回截至当前累计被丢冲量大小（L1 权重）之和。
func (w *World) DropWeight() int64 { return w.q.DropSum() }

// Hash 返回当前状态的指纹。
func (w *World) Hash() uint64 { return hash.State(w.tick, w.state) }

// Snapshot 返回可比较的状态切片，便于逐字节对比。
func (w *World) Snapshot() []int32 { return w.state.Raw() }
