// Package hash 提供状态指纹，按固定顺序混合所有数值。
package hash

import "goframe/internal/sim"

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
	for _, v := range s.Raw() {
		mix(uint64(uint32(v)))
	}
	return h
}
