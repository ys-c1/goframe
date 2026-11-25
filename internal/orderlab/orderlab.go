// Package orderlab 是确定性的“相乘顺序”实验台，只用于对拍、取证与归档，
// 不参与生产仿真。
//
// 它以与 internal/sim 相同的积分公式实现两条路径：
//
//	Canonical  v*(k*dt)   固定顺序，与生产路径逐位一致
//	Swapped    (v*k)*dt   故意调换顺序的反例路径
//
// Canonical 路径的正确性由 orderlab_test.go 与生产 api.World 逐帧对拍保证；
// 一旦它和生产实现漂移，测试先失败，归档数据就不可信了。
package orderlab

import (
	"goframe/internal/fixed"
	"goframe/internal/hash"
)

// Order 选择三因子乘积的相乘顺序。
type Order int

const (
	// Canonical 是生产顺序：先结合 k*dt，再与 v 相乘。
	Canonical Order = iota
	// Swapped 是反例顺序：(v*k)*dt。只用于验证与取证。
	Swapped
)

// String 返回顺序约定的简短名字。
func (o Order) String() string {
	if o == Swapped {
		return "swapped"
	}
	return "canonical"
}

// Config 是实验参数，全部为定点原始值。
type Config struct {
	Bodies   int
	SubSteps int
	Gravity  fixed.Value
	Drag     fixed.Value // 每帧阻尼系数 k
}

// Push 是单实体冲量。
type Push struct {
	Body   int
	DX, DY fixed.Value
}

// Frame 是一帧的指令集合。
type Frame struct{ Pushes []Push }

// Divergence 描述两轨在某帧的首个不同数值。
type Divergence struct {
	Body      int    `json:"body"`
	Field     string `json:"field"`
	Canonical int32  `json:"canonical"`
	Swapped   int32  `json:"swapped"`
	Delta     int32  `json:"delta"`
}

// FrameSample 是一帧的两轨指纹与首个差异（无差异时 Divergence 为零值且 Same=true）。
type FrameSample struct {
	Tick      int         `json:"tick"`
	CanonHash uint64      `json:"canon_hash"`
	SwapHash  uint64      `json:"swap_hash"`
	Same      bool        `json:"same"`
	First     *Divergence `json:"first_divergence,omitempty"`
}

// Run 以指定顺序跑完整条轨迹，返回每帧的原始状态（按 sim.Raw 的顺序）。
func Run(cfg Config, frames []Frame, ord Order) [][]int32 {
	w := newWorld(cfg)
	out := make([][]int32, 0, len(frames))
	for _, f := range frames {
		w.step(f, ord)
		out = append(out, w.raw())
	}
	return out
}

// Compare 同时跑两条路径，逐帧记录指纹与首个差异，供归档与反例展示。
// 这样同一份输入只编码一次，两条路径的差别只可能来自相乘顺序。
func Compare(cfg Config, frames []Frame) []FrameSample {
	canon := Run(cfg, frames, Canonical)
	swapped := Run(cfg, frames, Swapped)
	out := make([]FrameSample, len(frames))
	for i := range frames {
		s := FrameSample{
			Tick:      i,
			CanonHash: hash.Raw(i, canon[i]),
			SwapHash:  hash.Raw(i, swapped[i]),
			Same:      true,
		}
		if d := firstDiff(canon[i], swapped[i]); d != nil {
			s.Same = false
			s.First = d
		}
		out[i] = s
	}
	return out
}

// rawFields 与 sim.Raw 字段顺序一致：每实体 posX,posY,velX,velY。
var rawFields = [4]string{"posX", "posY", "velX", "velY"}

func firstDiff(a, b []int32) *Divergence {
	for i := range a {
		if a[i] != b[i] {
			return &Divergence{
				Body:      i / 4,
				Field:     rawFields[i%4],
				Canonical: a[i],
				Swapped:   b[i],
				Delta:     a[i] - b[i],
			}
		}
	}
	return nil
}

type world struct {
	cfg                    Config
	posX, posY, velX, velY []fixed.Value
}

func newWorld(cfg Config) *world {
	n := cfg.Bodies
	return &world{
		cfg:  cfg,
		posX: make([]fixed.Value, n),
		posY: make([]fixed.Value, n),
		velX: make([]fixed.Value, n),
		velY: make([]fixed.Value, n),
	}
}

func (w *world) step(f Frame, ord Order) {
	for _, p := range f.Pushes {
		w.velX[p.Body] = w.velX[p.Body].Add(p.DX)
		w.velY[p.Body] = w.velY[p.Body].Add(p.DY)
	}
	for i := 0; i < w.cfg.SubSteps; i++ {
		w.integrate(ord)
	}
}

// integrate 与 sim.State.Integrate 保持同一公式，仅在三因子乘积处按 ord 分叉。
func (w *world) integrate(ord Order) {
	dt := fixed.One.Div(fixed.FromInt(w.cfg.SubSteps))
	g := w.cfg.Gravity.Div(fixed.FromInt(w.cfg.SubSteps))
	coef := fixed.Mul3Coef(w.cfg.Drag, dt) // k*dt，只算一次
	for i := range w.posX {
		var dragX, dragY fixed.Value
		switch ord {
		case Canonical:
			dragX = fixed.Mul3By(w.velX[i], coef)
			dragY = fixed.Mul3By(w.velY[i], coef)
		case Swapped:
			// 反例写法：先 v*k 再乘 dt。除顺序外与 Canonical 完全同式。
			dragX = w.velX[i].Mul(w.cfg.Drag).Mul(dt)
			dragY = w.velY[i].Mul(w.cfg.Drag).Mul(dt)
		}
		w.velX[i] = w.velX[i].Sub(dragX)
		w.velY[i] = w.velY[i].Add(g)
		w.velY[i] = w.velY[i].Sub(dragY)
		w.posX[i] = w.posX[i].Add(w.velX[i])
		w.posY[i] = w.posY[i].Add(w.velY[i])
	}
}

func (w *world) raw() []int32 {
	out := make([]int32, 0, 4*len(w.posX))
	for i := range w.posX {
		out = append(out, w.posX[i].Raw(), w.posY[i].Raw(), w.velX[i].Raw(), w.velY[i].Raw())
	}
	return out
}
