// Package hash 提供状态指纹。实体按全局单调编号升序混合，
// 且每个字都携带其编号，因此指纹只取决于"哪些编号以什么状态存活"，
// 与生成/销毁的先后顺序及存储槽位无关。
package hash

import (
	"goframe/internal/sim"
)

const offset = 14695981039346656037
const prime = 1099511628211

// State 返回帧号与状态的指纹。
func State(tick int, s *sim.State) uint64 {
	h := uint64(offset)
	mix := func(v uint64) {
		h ^= v
		h *= prime
	}
	mix(uint64(uint32(tick)))
	for _, b := range s.SortedBodies() {
		// 编号先入流：同一状态挂在不同编号上必然得到不同指纹，
		// 编号稀疏（中间有销毁留下的空洞）也不影响存活实体的相对顺序。
		mix(uint64(uint32(b.ID)))
		mix(uint64(uint32(b.PosX.Raw())))
		mix(uint64(uint32(b.PosY.Raw())))
		mix(uint64(uint32(b.VelX.Raw())))
		mix(uint64(uint32(b.VelY.Raw())))
	}
	return h
}
