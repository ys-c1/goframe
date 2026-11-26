// Package rollback 是在确定性内核之上的最小回滚引擎。
// 引擎只保留最近 Depth+1 帧的推进前状态快照（环形缓冲），
// 迟到超过 Depth 帧的输入无法回滚，记为越界失败。
package rollback

import (
	"errors"
	"fmt"

	"goframe/api"
)

// ErrOutOfWindow 是输入迟到深度超过缓冲深度，目标帧快照已被驱逐。
var ErrOutOfWindow = errors.New("回滚越界: 目标帧已超出环形缓冲窗口")

// EventKind 标记一次引擎动作。
type EventKind string

const (
	EventAdvance   EventKind = "advance"  // 正常推进一帧
	EventRollback  EventKind = "rollback" // 迟到输入命中窗口，回滚并重放
	EventOutOfWind EventKind = "oob"      // 迟到输入落在窗口外，丢输入
	EventDiverge   EventKind = "diverge"  // 重放后哈希与权威序列不一致
)

// Event 是引擎日志的一行，全部字段可直接落盘复现。
type Event struct {
	Kind     EventKind
	AtTick   int    // 事件发生时引擎的当前帧
	Target   int    // 迟到输入的生效帧（advance 时等于 AtTick）
	Delay    int    // 输入延迟，单位帧
	Depth    int    // 引擎配置的回滚深度
	WindowLo int    // 当前环形缓冲保留的最老帧
	WindowHi int    // 当前环形缓冲最新帧
	Hash     uint64 // 事件后当前帧哈希
	Note     string // 补充说明，例如越界时的帧号关系
}

// Engine 持有一个确定性世界、每帧输入和深度有限的状态环。
// 环中 tick=t 的快照是第 t 帧推进前的世界状态（等价于第 t-1 帧推进后）。
type Engine struct {
	world  *api.World
	depth  int
	ring   []api.StateSnap // 容量 depth+1，存推进前快照
	head   int             // ring 中最老帧的下标，空环时为 0
	size   int             // 环内快照数
	inputs [][]api.Command // 按帧号存放的权威输入
	events []Event
	// trusted[t] 是无丢输入情况下第 t 帧推进后的权威哈希，用于对拍。
	trusted  []uint64
	oobCount int
}

// New 创建回滚深度为 depth 的引擎。
func New(opt api.Options, depth int) (*Engine, error) {
	w, err := api.New(opt)
	if err != nil {
		return nil, err
	}
	if depth < 0 {
		return nil, fmt.Errorf("depth 不能为负: %d", depth)
	}
	return &Engine{
		world: w,
		depth: depth,
		ring:  make([]api.StateSnap, depth+1),
	}, nil
}

// Events 返回截至目前的全部引擎事件。
func (e *Engine) Events() []Event { return e.events }

// Tick 返回引擎当前帧号。
func (e *Engine) Tick() int { return e.world.Tick() }

// Hash 返回当前状态指纹。
func (e *Engine) Hash() uint64 { return e.world.Hash() }

// Window 返回环形缓冲保留的最老帧与最新帧；空环以 (-1,-1) 表示。
func (e *Engine) Window() (lo, hi int) {
	if e.size == 0 {
		return -1, -1
	}
	lo = e.ring[e.head].Tick
	hi = e.ring[(e.head+e.size-1)%len(e.ring)].Tick
	return lo, hi
}

// pushSnap 把当前世界状态以 tick 号存入环形缓冲，超出容量即驱逐最老帧。
func (e *Engine) pushSnap(tick int) {
	slot := (e.head + e.size) % len(e.ring)
	snap := e.world.SaveState()
	snap.Tick = tick
	e.ring[slot] = snap
	if e.size == len(e.ring) {
		e.head = (e.head + 1) % len(e.ring) // 环满，最老帧被覆盖
	} else {
		e.size++
	}
}

// snapAt 取帧号 t 的推进前快照，不存在返回 ok=false。
func (e *Engine) snapAt(t int) (api.StateSnap, bool) {
	if e.size == 0 {
		return api.StateSnap{}, false
	}
	lo, hi := e.Window()
	if t < lo || t > hi {
		return api.StateSnap{}, false
	}
	idx := (e.head + (t - lo)) % len(e.ring)
	return e.ring[idx], true
}

// recordInput 把生效帧 t 的输入写入按帧索引的切片。
func (e *Engine) recordInput(t int, cmds []api.Command) {
	for len(e.inputs) <= t {
		e.inputs = append(e.inputs, nil)
	}
	e.inputs[t] = append(e.inputs[t], cmds...)
}

// SetTrusted 注入权威哈希序列（无丢输入重放得到），用于重放对拍。
func (e *Engine) SetTrusted(hashes []uint64) {
	e.trusted = append(e.trusted[:0], hashes...)
}

// Advance 按时钟推进一帧：先把推进前状态入环，再施加本帧已知输入并积分。
// 环中因此始终保存当前帧与前 depth 帧的推进前快照，可直接回滚。
func (e *Engine) Advance(cmds []api.Command) error {
	t := e.world.Tick()
	e.pushSnap(t)
	e.recordInput(t, cmds)
	if err := e.world.ApplyCommands(cmds); err != nil {
		return err
	}
	e.world.IntegrateFrame()
	lo, hi := e.Window()
	e.events = append(e.events, Event{
		Kind: EventAdvance, AtTick: e.world.Tick(), Target: t,
		WindowLo: lo, WindowHi: hi, Hash: e.world.Hash(),
	})
	return nil
}

// Inject 注入一条在 atTick 时刻才到达、生效帧为 target 的远端输入，
// delay 为 atTick-target 的帧延迟。target 的推进前快照还在窗口内就回滚重放，
// 否则记一次越界失败并丢输入。
func (e *Engine) Inject(atTick, target int, cmds []api.Command, delay int) error {
	lo, hi := e.Window()
	snap, ok := e.snapAt(target)
	if !ok {
		e.oobCount++
		e.events = append(e.events, Event{
			Kind: EventOutOfWind, AtTick: atTick, Target: target, Delay: delay,
			Depth: e.depth, WindowLo: lo, WindowHi: hi,
			Note: fmt.Sprintf("target=%d < windowLo=%d：第 %d 帧的推进前快照已在窗口滑动时被驱逐，输入被丢弃，帧 %d 起状态永久偏离权威序列",
				target, lo, target, lo),
		})
		return fmt.Errorf("%w: atTick=%d target=%d window=[%d,%d] depth=%d",
			ErrOutOfWindow, atTick, target, lo, hi, e.depth)
	}

	// 命中窗口：恢复到 target 帧推进前，登记迟到输入，从 target 重放到当前帧。
	if err := e.world.RestoreState(snap); err != nil {
		return err
	}
	e.recordInput(target, cmds)
	cur := atTick
	replayed := 0
	for t := target; t < cur; t++ {
		var cmds []api.Command
		if t < len(e.inputs) {
			cmds = e.inputs[t]
		}
		if err := e.world.ApplyCommands(cmds); err != nil {
			return err
		}
		e.world.IntegrateFrame()
		replayed++
		// 推进前快照不随重放改变（它是 t 帧积分前的状态），环内容保持有效。
	}
	lo2, hi2 := e.Window()
	ev := Event{
		Kind: EventRollback, AtTick: atTick, Target: target, Delay: delay,
		Depth: e.depth, WindowLo: lo2, WindowHi: hi2, Hash: e.world.Hash(),
		Note: fmt.Sprintf("回滚 %d 帧，重放 %d 帧至 tick=%d", cur-target, replayed, cur),
	}
	e.events = append(e.events, ev)
	// 对拍：仅在此前没有越界丢输入时才与权威哈希比较。
	// 一旦发生过 OOB，权威输入序列已不可达，后续偏差是首次越界的必然后果。
	if e.oobCount == 0 && cur-1 >= 0 && cur-1 < len(e.trusted) && e.world.Hash() != e.trusted[cur-1] {
		e.events = append(e.events, Event{
			Kind: EventDiverge, AtTick: atTick, Target: target, Delay: delay,
			Depth: e.depth, WindowLo: lo2, WindowHi: hi2, Hash: e.world.Hash(),
			Note: fmt.Sprintf("重放哈希 %016x 与权威哈希 %016x 不一致",
				e.world.Hash(), e.trusted[cur-1]),
		})
	}
	return nil
}
