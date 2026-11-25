// Package predict 实现“客户端本地预测 + 服务器权威纠正”的确定性模型。
//
// 模型沿用内核的 Q16.16 定点与逐帧积分规则：
//   - Server 是权威端，每帧对实体施加客户端看不到的权威修正 nudge（模拟
//     碰撞/摩擦等只有服务器处理的效果），产出 Authority 帧。
//   - Client 每帧先本地预测；收到延迟 Lead 帧的权威帧后回放纠正。
//   - Client 只保留最近 Replay 帧的快照环形缓冲。权威帧指向的帧号
//     已被淘汰时无法回溯，纠正被丢弃——这正是“重放帧数不足”的情形。
package predict

import (
	"fmt"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

// Config 是一次预测/纠正实验的参数。
type Config struct {
	Bodies       int         // 实体数
	Lead         int         // 预测提前量 N：权威帧延迟 N 帧到达客户端
	Replay       int         // 重放帧数 R：客户端环形缓冲保留的历史帧数
	InitialVelX  fixed.Value // 初始 X 速度（匀速滑行，无本地输入）
	ServerNudgeV fixed.Value // 服务器每帧施加的权威速度修正（客户端预测不到）
}

// Authority 是服务器在 Tick 帧结束时的权威状态。
type Authority struct {
	Tick int
	*sim.State
}

// Server 是权威端。
type Server struct {
	cfg   Config
	state *sim.State
	tick  int
}

// NewServer 创建权威端。
func NewServer(cfg Config) *Server {
	s := sim.NewState(cfg.Bodies, fixed.FromInt(0))
	for i := 0; i < cfg.Bodies; i++ {
		s.Push(i, cfg.InitialVelX, fixed.FromInt(0))
	}
	return &Server{cfg: cfg, state: s}
}

// Tick 返回服务器当前帧号。
func (s *Server) Tick() int { return s.tick }

// State 返回权威状态（只读使用）。
func (s *Server) State() *sim.State { return s.state }

// Step 推进一帧：先施加客户端预测不到的权威修正，再积分。
// 返回的 Authority 是该帧结束时的独立快照——权威帧要在网络上延迟
// Lead 帧投递，不能持有服务器的活状态指针，否则客户端读到的是未来状态。
func (s *Server) Step() Authority {
	for i := 0; i < s.cfg.Bodies; i++ {
		s.state.Push(i, s.cfg.ServerNudgeV, fixed.FromInt(0))
	}
	s.state.Integrate(1)
	s.tick++
	return Authority{Tick: s.tick, State: s.state.Clone()}
}

// Ring 是客户端的定长历史环形缓冲。
// Replay=R 表示客户端可以回溯到 R 帧之前：缓冲覆盖 [t-R, t] 共 R+1 个
// 帧槽（当前帧 + R 个历史帧）。槽位按 tick 取模索引；被覆盖的帧即淘汰。
type Ring struct {
	slots []*sim.State
	ticks []int
}

// NewRing 按可回溯帧数 R 创建环形缓冲（实际槽位 R+1）。
func NewRing(r int) *Ring {
	if r < 1 {
		r = 1
	}
	cap := r + 1
	return &Ring{slots: make([]*sim.State, cap), ticks: make([]int, cap)}
}

// Put 写入帧号 tick 的状态快照（独立拷贝）。
func (r *Ring) Put(tick int, st *sim.State) {
	i := tick % len(r.slots)
	r.slots[i] = st.Clone()
	r.ticks[i] = tick
}

// Get 返回帧号为 tick 的快照；已被淘汰或尚未写入时返回 nil。
func (r *Ring) Get(tick int) *sim.State {
	i := tick % len(r.slots)
	if r.ticks[i] != tick {
		return nil
	}
	return r.slots[i]
}

// Oldest 返回当前缓冲中最老一帧的帧号；空缓冲返回 -1。
func (r *Ring) Oldest() int {
	oldest := -1
	for i, t := range r.ticks {
		if r.slots[i] == nil {
			continue
		}
		if oldest == -1 || t < oldest {
			oldest = t
		}
	}
	return oldest
}

// 渲染换算：1 个 Q16.16 世界单位 = 32 像素，故 1 像素 = 2048 raw。
const pixelsPerUnit = 32.0

func rawToPx(raw int32) float64 {
	return float64(raw) / float64(fixed.One.Raw()) * pixelsPerUnit
}

func absPx(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Client 是做本地预测与回放纠正的客户端。
type Client struct {
	cfg   Config
	state *sim.State
	tick  int
	ring  *Ring

	resyncs int // 帧淘汰后硬对齐权威状态的次数

	maxSnapPx   float64 // 回放纠正瞬间的最大画面跳跃（像素）
	maxResyncPx float64 // 硬对齐瞬间的最大画面跳跃（像素）
	maxGapPx    float64 // 客户端渲染位置与权威位置的最大偏差（像素）
}

// NewClient 创建客户端。
func NewClient(cfg Config) *Client {
	st := sim.NewState(cfg.Bodies, fixed.FromInt(0))
	for i := 0; i < cfg.Bodies; i++ {
		st.Push(i, cfg.InitialVelX, fixed.FromInt(0))
	}
	return &Client{cfg: cfg, state: st, ring: NewRing(cfg.Replay)}
}

// Tick 返回客户端当前帧号。
func (c *Client) Tick() int { return c.tick }

// State 返回客户端当前（渲染）状态。
func (c *Client) State() *sim.State { return c.state }

// Resyncs 返回因重放帧数不足而触发硬对齐的次数。
func (c *Client) Resyncs() int { return c.resyncs }

// Predict 本地预测一帧：无本地输入，直接按当前速度积分并存快照。
func (c *Client) Predict() {
	c.state.Integrate(1)
	c.tick++
	c.ring.Put(c.tick, c.state)
}

// Reconcile 处理一个延迟到达的权威帧，返回跳跃幅度（像素）与纠正方式。
//
// 权威帧指向的帧号仍在环形缓冲内（R≥N）：回溯到该帧并用权威状态替换，
// 逐帧重放到当前帧——平滑回放，mode="replay"。
//
// 权威帧指向的帧号已被淘汰（R<N）：无法回溯，只能把客户端当前状态硬对齐
// 到这个“迟到的权威帧”，再由后续预测继续——画面跳变，mode="resync"。
// 这正是重放帧数不足时客户端的退路。
func (c *Client) Reconcile(a Authority) (jumpPx float64, mode string, err error) {
	if a.Tick > c.tick {
		return 0, "", fmt.Errorf("权威帧 %d 超前于客户端 %d", a.Tick, c.tick)
	}
	beforeX := c.state.PosX(0).Raw()
	if base := c.ring.Get(a.Tick); base != nil {
		// 回溯重放：从权威帧出发，用缓冲的历史输入逐帧推到当前帧。
		// 本实验无本地输入，重放即按权威速度逐帧积分。
		roll := a.State.Clone()
		for t := a.Tick; t < c.tick; t++ {
			roll.Integrate(1)
		}
		c.state = roll
		c.ring.Put(c.tick, c.state)
		jumpPx = absPx(rawToPx(c.state.PosX(0).Raw() - beforeX))
		if jumpPx > c.maxSnapPx {
			c.maxSnapPx = jumpPx
		}
		return jumpPx, "replay", nil
	}
	// 帧已淘汰：硬对齐到迟到的权威帧。
	c.state = a.State.Clone()
	c.ring.Put(a.Tick, c.state)
	c.resyncs++
	jumpPx = absPx(rawToPx(c.state.PosX(0).Raw() - beforeX))
	if jumpPx > c.maxResyncPx {
		c.maxResyncPx = jumpPx
	}
	return jumpPx, "resync", nil
}

// ObserveGap 记录客户端当前渲染位置与给定权威帧的偏差（像素）。
func (c *Client) ObserveGap(a Authority) {
	g := absPx(rawToPx(c.state.PosX(0).Raw() - a.State.PosX(0).Raw()))
	if g > c.maxGapPx {
		c.maxGapPx = g
	}
}

// MaxSnapPx 返回回放纠正瞬间的最大跳跃（像素）。
func (c *Client) MaxSnapPx() float64 { return c.maxSnapPx }

// MaxResyncPx 返回硬对齐瞬间的最大跳跃（像素）。
func (c *Client) MaxResyncPx() float64 { return c.maxResyncPx }

// MaxGapPx 返回客户端与权威渲染位置的最大偏差（像素）。
func (c *Client) MaxGapPx() float64 { return c.maxGapPx }
