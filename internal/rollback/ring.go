// Package rollback 实现定长回滚帧环。
//
// 旧方案保留 5 份全量快照，固定占用 5*FullSize(N)。
// 现在环里只存每一帧的“回滚差分”规范编码（编号+旧值），
// 第 j 帧占 DeltaSize(N,k_j)=12+6*k_j 字节，k_j 是该帧实际改动字段数。
// 活跃仿真状态本身就是链头，因此不需要额外常驻一份全量基线。
package rollback

import "errors"

// Frames 是题目约定的回滚帧数。
const Frames = 5

// ErrEmpty 表示环中没有可回滚的帧。
var ErrEmpty = errors.New("回滚帧环已空")

// Blob 是环中一帧：帧前 tick 与差分的规范二进制编码。
type Blob struct {
	TickBefore int
	Codec      []byte
}

// Ring 是容量 cap 的 FIFO 帧环；Push 超出容量时丢弃最旧一帧。
type Ring struct {
	capacity int
	frames   []Blob // 时间升序，frames[len-1] 是最新一帧
}

// NewRing 创建容量 capacity 的环；capacity<=0 时使用默认 5。
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = Frames
	}
	return &Ring{capacity: capacity}
}

// NewFive 创建五帧环。
func NewFive() *Ring { return NewRing(Frames) }

// Capacity 返回环容量。
func (r *Ring) Capacity() int { return r.capacity }

// Len 返回当前保留的帧数。
func (r *Ring) Len() int { return len(r.frames) }

// Push 压入新一帧，超容量时挤出最旧帧。Codec 会被复制，避免调用方复用底层数组。
func (r *Ring) Push(b Blob) {
	cp := append([]byte(nil), b.Codec...)
	b.Codec = cp
	if len(r.frames) == r.capacity {
		copy(r.frames, r.frames[1:])
		r.frames[len(r.frames)-1] = b
		return
	}
	r.frames = append(r.frames, b)
}

// Pop 弹出最新一帧（LIFO 回滚顺序）。
func (r *Ring) Pop() (Blob, bool) {
	n := len(r.frames)
	if n == 0 {
		return Blob{}, false
	}
	b := r.frames[n-1]
	r.frames = r.frames[:n-1]
	return b, true
}

// Bytes 返回当前保留的全部差分编码字节数（不含结构体/切片头开销）。
func (r *Ring) Bytes() int {
	sum := 0
	for _, f := range r.frames {
		sum += len(f.Codec)
	}
	return sum
}

// All 按时间从旧到新返回帧副本，供对照落盘使用。
func (r *Ring) All() []Blob {
	out := make([]Blob, len(r.frames))
	for i, f := range r.frames {
		out[i] = Blob{TickBefore: f.TickBefore, Codec: append([]byte(nil), f.Codec...)}
	}
	return out
}
