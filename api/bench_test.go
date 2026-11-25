package api

import (
	"fmt"
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

// BenchmarkFrameVsDedup 用真实帧参数（默认 8 实体、4 子步）对比：
// 一帧完整推进（含把 k 条指令施加到状态并积分）vs 只对同样 k 条指令做归并。
func BenchmarkFrameVsDedup(b *testing.B) {
	opt := Options{Bodies: 8, SubSteps: 4, Gravity: 2}
	for _, k := range []int{1, 8, 32} {
		cmds := make([]Command, k)
		for i := range cmds {
			cmds[i] = Command{Tick: 0, Body: i % opt.Bodies, PushX: 1 << 12}
		}
		b.Run(fmt.Sprintf("dedup/k=%d", k), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = DedupCommands(cmds)
			}
		})
		b.Run(fmt.Sprintf("frame/k=%d", k), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				w, _ := New(opt)
				_ = w.Step(cmds)
			}
		})
	}
}
func BenchmarkDedupVsExecute(b *testing.B) {
	for _, n := range []int{8, 64, 512} {
		cmds := make([]Command, n)
		for i := range cmds {
			cmds[i] = Command{Tick: 0, Body: i % 4, PushX: 1 << 12}
		}
		b.Run(fmt.Sprintf("dedup/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = DedupCommands(cmds)
			}
		})
		b.Run(fmt.Sprintf("dedupInPlace/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			buf := append([]Command(nil), cmds...)
			for i := 0; i < b.N; i++ {
				copy(buf[:len(cmds)], cmds)
				_ = DedupInPlace(buf[:len(cmds)])
			}
		})
		b.Run(fmt.Sprintf("execute/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			s := sim.NewState(4, 0)
			for i := 0; i < b.N; i++ {
				for _, c := range cmds {
					s.Push(c.Body, fixed.FromRaw(c.PushX), fixed.FromRaw(c.PushY))
				}
			}
		})
	}
}
