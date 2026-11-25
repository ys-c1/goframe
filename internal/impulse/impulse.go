// Package impulse 提供重力冲量在子步/帧之间的精确整数分摊与余数台账。
//
// 所有数值都用 int64 表示 Q16.16 的 raw 单位（1 定点单位 = 1<<16 raw），
// 因此证明里的恒等式可以被逐数执行验证，不涉及浮点。
package impulse

// Euclid 返回 floor 商 q 与欧几里得余数 r，恒有 total = n*q + r 且 0 <= r < n。
// n 必须为正。对负的 total 也成立（余数仍非负）。
func Euclid(total, n int64) (q, r int64) {
	if n <= 0 {
		panic("impulse: n must be positive")
	}
	q = total / n
	r = total - q*n
	if r < 0 { // Go 的整除向零取整，负数时余数为负，修正成 floor 口径
		r += n
		q--
	}
	return q, r
}

// Naive 复现修复前内核的做法：每个子步都取 trunc(total/n)，余数直接丢弃。
// 返回 n 个相同的子步冲量、整帧实际下发量 delivered 与丢失量 loss。
func Naive(total, n int64) (steps []int64, delivered, loss int64) {
	q, _ := Euclid(total, n)
	steps = make([]int64, n)
	for i := range steps {
		steps[i] = q
	}
	delivered = n * q
	loss = total - delivered
	return steps, delivered, loss
}

// Exact 把 total 严格分摊到 n 个整数子步上：n 份 q，再把余数 r 拆成 r 个 +1。
// 返回切片的元素之和在整数意义下严格等于 total。
// +1 放在末尾的 r 个子步，保证前 n-r 个子步与旧的整除结果完全一致。
func Exact(total, n int64) []int64 {
	q, r := Euclid(total, n)
	steps := make([]int64, n)
	for i := int64(0); i < n; i++ {
		steps[i] = q
		if i >= n-r {
			steps[i]++
		}
	}
	return steps
}

// FrameCarry 是“把本帧整除不下的余数记入下一帧”的跨帧台账。
//
// 不变量（每一帧都成立，而不是只在末帧成立）：
//
//	G + RemIn = Delivered + RemOut
//
// 其中 RemOut 成为下一帧的 RemIn。把各帧相加并望远镜消去后：
//
//	sum Delivered[0:F] = F*G - RemOut(F) + RemIn(0)
//
// 若初始 Rem=0，累计下发与 F*G 的差恰好是当前未结清的余数 Rem，
// 且 0 <= Rem < n，因此任意截断时刻的误差都有界。
type FrameCarry struct {
	// Rem 是从上一帧结转来、尚未变成速度冲量的余数（raw）。
	Rem int64
}

// CarryFrame 推进一帧：把设定值 G 加上期初余数后再按 n 个子步整除，
// 每步下发相同的 q，期末余数记入 c.Rem。
// 返回每子步冲量 q、整帧下发量 delivered 与期末余数 remOut。
func (c *FrameCarry) CarryFrame(G, n int64) (q, delivered, remIn, remOut int64) {
	remIn = c.Rem
	budget := G + remIn
	q, remOut = Euclid(budget, n)
	c.Rem = remOut
	delivered = n * q
	return q, delivered, remIn, remOut
}

// IdentityDiff 校验单帧复式守恒恒等式，返回应当恒为 0 的差值：
// G + remIn - delivered - remOut。
func IdentityDiff(G, delivered, remIn, remOut int64) int64 {
	return G + remIn - delivered - remOut
}
