// Package hash 提供状态指纹，按固定顺序混合所有数值。
package hash

import "goframe/internal/sim"

const offset = 14695981039346656037
const prime = 1099511628211

// fnv 在给定的字段截断函数下做 FNV-1a 混合。truncate 为 nil 时保留完整 32 位。
func fnv(tick int, s *sim.State, truncate func(uint32) uint32) uint64 {
	h := uint64(offset)
	mix := func(v uint64) {
		h ^= v
		h *= prime
	}
	t := uint32(tick)
	if truncate != nil {
		t = truncate(t)
	}
	mix(uint64(t))
	for _, v := range s.Raw() {
		u := uint32(v)
		if truncate != nil {
			u = truncate(u)
		}
		mix(uint64(u))
	}
	return h
}

// State 返回帧号与状态的指纹，混合每个字段的完整 32 位，低位不丢。
func State(tick int, s *sim.State) uint64 {
	return fnv(tick, s, nil)
}

// StateDropLow 模拟“丢掉每个字段最低 dropBits 位再混合”的旧版窄位哈希：
// 字段先按桶宽 B=2^dropBits 量化（保留高 32-dropBits 位），桶内小幅差异不可见。
// dropBits=0 时与 State 完全相同。
func StateDropLow(tick int, s *sim.State, dropBits uint) uint64 {
	if dropBits == 0 {
		return State(tick, s)
	}
	return fnv(tick, s, func(v uint32) uint32 { return v >> dropBits << dropBits })
}

// StateKeepLow 模拟字面意义上的“哈希只取低位”：每个字段只保留最低 keepBits 位，
// 2^keepBits 的整数倍差异（即只翻动第 keepBits 位及以上的“整数单位级”差异）不可见。
func StateKeepLow(tick int, s *sim.State, keepBits uint) uint64 {
	if keepBits >= 32 {
		return State(tick, s)
	}
	mask := uint32(1)<<keepBits - 1
	return fnv(tick, s, func(v uint32) uint32 { return v & mask })
}
