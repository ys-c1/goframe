package sim

import (
	"fmt"

	"goframe/internal/fixed"
)

// SetRaw 按 Raw() 的规范交错序 [posX,posY,velX,velY]*B 覆盖状态。
// 长度必须为 4*Len()。快照迁移/回放恢复走这里，保证与直跑共用同一内核。
func (s *State) SetRaw(raw []int32) error {
	if want := 4 * s.Len(); len(raw) != want {
		return fmt.Errorf("raw 字段数不符: got %d, want %d", len(raw), want)
	}
	for i := 0; i < s.Len(); i++ {
		s.posX[i] = fixed.FromRaw(int(raw[4*i+0]))
		s.posY[i] = fixed.FromRaw(int(raw[4*i+1]))
		s.velX[i] = fixed.FromRaw(int(raw[4*i+2]))
		s.velY[i] = fixed.FromRaw(int(raw[4*i+3]))
	}
	return nil
}

// SetTickGravity 仅用于恢复路径保持重力参数一致；重力本就由 Options 重建，
// 这里保留一个显式入口以便未来快照携带重力时校验。
func (s *State) SetGravity(g fixed.Value) { s.gravity = g }
