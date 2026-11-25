// Package hash 提供状态指纹，按固定顺序混合所有数值。
package hash

import "goframe/internal/sim"

const offset = 14695981039346656037
const prime = 1099511628211

// State 返回帧号与状态的指纹。
//
// 每个 int32 字段（Q16.16 定点值，32 位全部承载有效信息）必须整体参与混合，
// 不能在混合前截到更窄的低位。否则两个"低位相同、高位不同"的状态会喂给
// 混合器完全相同的输入，FNV-1a 的 XOR/乘法只依赖当前输入，于是必然得到
// 同一指纹，回放对拍把分叉误判成一致。
//
// int32 -> uint64 是符号扩展的无损映射（不同 int32 必映射到不同 uint64），
// 高位不会被丢，修复即让字段的每一位都影响最终乘积。
func State(tick int, s *sim.State) uint64 {
	h := uint64(offset)
	mix := func(v uint64) {
		h ^= v
		h *= prime
	}
	mix(uint64(int64(tick)))
	for _, v := range s.Raw() {
		mix(uint64(int64(v)))
	}
	return h
}
