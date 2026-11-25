// Package sim 是确定性积分内核，只做加法与定点乘法。
package sim

import "goframe/internal/fixed"

// guardBits 是内部宽精度表示在 Q16.16 之下额外保留的二进制位数。
//
// 状态对外仍以 Q16.16（fixed.Value）导出并参与哈希，但子步积分在
// Q(16+guardBits).(16+guardBits) 的 int64 宽域中进行，使得 2 的幂子步长
// h=1/sub（sub<=2^guardBits）对应的除法与中点漂移不丢失任何低位，
// 从而让“同一帧末态”逐位与子步数无关。
const guardBits = 16

// State 是所有实体的位置与速度，按固定顺序存放，便于逐字节比较。
//
// posX/.../gravity 是对外的 Q16.16 镜像，保证 Raw() 与旧调用方式不变；
// wPosX/.../wGravity 是内部权威宽精度状态，跨子步保留保护位，避免每子步
// 量化到 Q16.16 带来的、随子步数变化的舍入。
type State struct {
	posX, posY []fixed.Value
	velX, velY []fixed.Value
	gravity    fixed.Value

	wPosX, wPosY []int64
	wVelX, wVelY []int64
	wGravity     int64
}

// NewState 创建实体数为 n 的状态。
func NewState(n int, gravity fixed.Value) *State {
	s := &State{
		posX:     make([]fixed.Value, n),
		posY:     make([]fixed.Value, n),
		velX:     make([]fixed.Value, n),
		velY:     make([]fixed.Value, n),
		gravity:  gravity,
		wPosX:    make([]int64, n),
		wPosY:    make([]int64, n),
		wVelX:    make([]int64, n),
		wVelY:    make([]int64, n),
		wGravity: int64(gravity.Raw()) << guardBits,
	}
	return s
}

// Len 返回实体数。
func (s *State) Len() int { return len(s.posX) }

// Push 给实体施加冲量。
func (s *State) Push(i int, dx, dy fixed.Value) {
	// 冲量是 Q16.16 整数倍，左移 guardBits 写入宽域，再统一重建镜像。
	s.wVelX[i] += int64(dx.Raw()) << guardBits
	s.wVelY[i] += int64(dy.Raw()) << guardBits
	s.sync(i)
}

// sync 把第 i 个实体的宽域状态向下量化为 Q16.16 镜像。
// 使用算术右移（向负无穷取整），对正负输入都是确定且跨子步一致的。
func (s *State) sync(i int) {
	s.posX[i] = fixed.FromRaw(int(s.wPosX[i] >> guardBits))
	s.posY[i] = fixed.FromRaw(int(s.wPosY[i] >> guardBits))
	s.velX[i] = fixed.FromRaw(int(s.wVelX[i] >> guardBits))
	s.velY[i] = fixed.FromRaw(int(s.wVelY[i] >> guardBits))
}

// Integrate 执行“一个子步”的显式中点格式，sub 为本帧的总子步数，
// 因此本子步长度 h=1/sub（帧长归一化为 1）。
//
// 中点格式（a 为本帧常量重力）：
//
//	v_mid = v + a*h/2
//	x    += h * v_mid
//	v    += a*h
//
// 全部在宽精度域用整数除法完成。当 sub 为 2 的幂时这些除法不引入随 n 变化的
// 舍入：在 guardBits=16 下，重力/速度踢动的除法对 sub<=2^16 精确，中点漂移
// vmidY/sub 对任意 Q16 原始输入在 sub<=128（2^7）内精确；Options 上限为 16，
// 故 1/2/4/8/16 全部保证。此时 n 个子步严格望远镜相消为同一帧映射：
//
//	v' = v + a
//	x' = x + v + a/2
//
// 因而同一帧末态 Raw()/Hash() 与 sub 无关。非 2 的幂 sub 仍给出合法中点积分，
// 但 1/sub 无法二进制精确表示，逐子步舍入可能累积，不保证哈希一致。
// 以上还要求常量加速度、帧内无间断事件且 int64 宽域不溢出。
func (s *State) Integrate(sub int) {
	if sub <= 0 {
		sub = 1
	}
	n := int64(sub)
	fullKick := s.wGravity / n       // a*h
	halfKick := s.wGravity / (2 * n) // a*h/2

	for i := range s.posX {
		vmidY := s.wVelY[i] + halfKick

		s.wPosX[i] += s.wVelX[i] / n // ax=0，中点速度即 vx
		s.wPosY[i] += vmidY / n
		s.wVelY[i] += fullKick

		s.sync(i)
	}
}

// IntegrateEuler 保留修复前的原始积分格式（半隐式 Euler），仅用于“子步数相关性”
// 的证明与对拍，不参与新内核的 Step 路径。
//
// 这是初始提交 3bb65f9 的逐字实现：重力按 sub 分摊，但位置漂移没有乘子步长 h。
func (s *State) IntegrateEuler(sub int) {
	g := s.gravity.Div(fixed.FromInt(sub))
	for i := range s.posX {
		s.velY[i] = s.velY[i].Add(g)
		s.posX[i] = s.posX[i].Add(s.velX[i])
		s.posY[i] = s.posY[i].Add(s.velY[i])

		// 保持宽域镜像与旧 Q16.16 结果一致（Euler 是有损参考路径）。
		s.wPosX[i] = int64(s.posX[i].Raw()) << guardBits
		s.wPosY[i] = int64(s.posY[i].Raw()) << guardBits
		s.wVelX[i] = int64(s.velX[i].Raw()) << guardBits
		s.wVelY[i] = int64(s.velY[i].Raw()) << guardBits
	}
}

// Raw 按固定顺序导出全部数值，便于逐字节比较。
func (s *State) Raw() []int32 {
	out := make([]int32, 0, 4*len(s.posX))
	for i := range s.posX {
		out = append(out, s.posX[i].Raw(), s.posY[i].Raw(), s.velX[i].Raw(), s.velY[i].Raw())
	}
	return out
}
