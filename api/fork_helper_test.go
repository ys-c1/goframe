package api

import (
	"goframe/internal/hash"
	"goframe/internal/sim"
)

// fingerprint 仅供测试：对指定帧号的 sim 状态计算对外同款指纹。
// 生产代码通过 World.Hash() 取得指纹，无需此入口。
func fingerprint(tick int, s *sim.State) uint64 {
	return hash.State(tick, s)
}
