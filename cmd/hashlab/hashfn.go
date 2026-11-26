// Package main 的 hashfn.go 定义实验用的两版哈希字段截断方式。
// 混合算法与 internal/hash 的 FNV-1a 同参数；两者逐位一致性由
// cmd/hashlab 的交叉验证测试保证（直接构造 sim.State 对拍）。
package main

const fnvOffset = 14695981039346656037
const fnvPrime = 1099511628211

// fnvMix 是与 internal/hash 同参数的 FNV-1a，字段截断方式由 trunc 控制（nil 表示全 32 位）。
func fnvMix(tick int, snap []int32, trunc func(uint32) uint32) uint64 {
	h := uint64(fnvOffset)
	mix := func(v uint64) {
		h ^= v
		h *= fnvPrime
	}
	t := uint32(tick)
	if trunc != nil {
		t = trunc(t)
	}
	mix(uint64(t))
	for _, v := range snap {
		u := uint32(v)
		if trunc != nil {
			u = trunc(u)
		}
		mix(uint64(u))
	}
	return h
}

// v2Hash 全 32 位 FNV（修复版，低位全部参与混合）。
func v2Hash(tick int, snap []int32) uint64 {
	return fnvMix(tick, snap, nil)
}

// dropLowHash 丢最低 b 位再混合的旧版窄位哈希（分桶模型，桶宽 B=2^b，保留 32-b 位）。
func dropLowHash(b uint) func(int, []int32) uint64 {
	return func(tick int, snap []int32) uint64 {
		if b == 0 {
			return fnvMix(tick, snap, nil)
		}
		return fnvMix(tick, snap, func(v uint32) uint32 { return v >> b << b })
	}
}

// keepLowHash 只保留最低 k 位再混合的字面“只取低位”哈希。
func keepLowHash(k uint) func(int, []int32) uint64 {
	return func(tick int, snap []int32) uint64 {
		if k >= 32 {
			return fnvMix(tick, snap, nil)
		}
		mask := uint32(1)<<k - 1
		return fnvMix(tick, snap, func(v uint32) uint32 { return v & mask })
	}
}

func u64hex(v uint64) string {
	const digits = "0123456789abcdef"
	buf := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		buf[i] = digits[v&0xf]
		v >>= 4
	}
	return string(buf)
}
