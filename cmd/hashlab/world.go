// Package main 的 world.go 负责构造“真实分叉”的一对世界并逐帧取证。
package main

import (
	"goframe/api"
	"goframe/internal/fixed"
)

// deltaMode 决定位置差随帧的演化方式。
type deltaMode int

const (
	modeStable  deltaMode = iota // 稳定差：冲量后立即反向冲量抵消，位置差恒为 Δ
	modeGrowing                  // 增长差：只冲量不抵消，tick≥forkTick 时 Δ(t)=(t-forkTick+1)Δ
)

// forkTick 是位置差首次出现的帧号：第 4 帧的指令制造速度差，该帧积分后位置第一次不同。
const forkTick = 4

// FrameRow 是一帧的完整取证行：真实快照对比与两版哈希判定同时记录。
type FrameRow struct {
	Tick          int    `json:"tick"`
	PosA          int32  `json:"pos_a_raw"`
	PosB          int32  `json:"pos_b_raw"`
	DeltaRaw      int32  `json:"delta_raw"`
	SnapshotEqual bool   `json:"snapshot_equal"`
	HashA         string `json:"hash_a"`
	HashB         string `json:"hash_b"`
	HashEqual     bool   `json:"hash_equal"`
	V2HashA       string `json:"v2_hash_a"`
	V2HashB       string `json:"v2_hash_b"`
	V2HashEqual   bool   `json:"v2_hash_equal"`
}

// buildForkedWorlds 返回仅在第 forkTick 帧一条冲量上不同的一对世界。
//   - stable：B 在该帧多冲 +Δ，下一帧冲 -Δ，速度归零、位置差恒为 Δ；
//   - growing：B 只多冲 +Δ，速度差保持，位置差每帧增长一个 Δ。
//
// 两世界实体数、其余指令完全一致，因此这是一对“真实分叉”的确定性世界。
func buildForkedWorlds(deltaRaw int, mode deltaMode) (a, b *api.World, err error) {
	opt := api.Options{Bodies: 2, SubSteps: 1, Gravity: 0}
	if a, err = api.New(opt); err != nil {
		return nil, nil, err
	}
	if b, err = api.New(opt); err != nil {
		return nil, nil, err
	}

	frames := 0
	if mode == modeStable {
		frames = forkTick + 1 // 需要第 5 帧施加反向冲量
	} else {
		frames = forkTick
	}
	for t := 1; t <= frames; t++ {
		cmdsA := []api.Command(nil)
		cmdsB := []api.Command(nil)
		if t == forkTick {
			cmdsB = []api.Command{{Tick: t, Body: 0, PushX: deltaRaw}}
		}
		if mode == modeStable && t == forkTick+1 {
			cmdsB = []api.Command{{Tick: t, Body: 0, PushX: -deltaRaw}}
		}
		if err = a.Step(cmdsA); err != nil {
			return nil, nil, err
		}
		if err = b.Step(cmdsB); err != nil {
			return nil, nil, err
		}
	}
	return a, b, nil
}

// trace 从同一初始状态重新步进两个世界到 window 帧，逐帧记录真实位置差与两版哈希判定。
// oldHash 注入窄位哈希（drop-low 或 keep-low），newHash 固定为全 32 位 v2。
func trace(deltaRaw int, mode deltaMode, window int,
	oldHash func(tick int, snap []int32) uint64,
	newHash func(tick int, snap []int32) uint64) ([]FrameRow, error) {

	opt := api.Options{Bodies: 2, SubSteps: 1, Gravity: 0}
	wa, err := api.New(opt)
	if err != nil {
		return nil, err
	}
	wb, err := api.New(opt)
	if err != nil {
		return nil, err
	}

	rows := make([]FrameRow, 0, window)
	for t := 1; t <= window; t++ {
		var cmdsB []api.Command
		if t == forkTick {
			cmdsB = []api.Command{{Tick: t, Body: 0, PushX: deltaRaw}}
		}
		if mode == modeStable && t == forkTick+1 {
			cmdsB = []api.Command{{Tick: t, Body: 0, PushX: -deltaRaw}}
		}
		if err := wa.Step(nil); err != nil {
			return nil, err
		}
		if err := wb.Step(cmdsB); err != nil {
			return nil, err
		}

		snapA := wa.Snapshot()
		snapB := wb.Snapshot()
		posA := snapA[0] // 实体 0 的 posX 是 Raw() 的第 0 个字段
		posB := snapB[0]

		ohA := oldHash(t, snapA)
		ohB := oldHash(t, snapB)
		nhA := newHash(t, snapA)
		nhB := newHash(t, snapB)

		rows = append(rows, FrameRow{
			Tick:          t,
			PosA:          posA,
			PosB:          posB,
			DeltaRaw:      posB - posA,
			SnapshotEqual: equalSnap(snapA, snapB),
			HashA:         u64hex(ohA),
			HashB:         u64hex(ohB),
			HashEqual:     ohA == ohB,
			V2HashA:       u64hex(nhA),
			V2HashB:       u64hex(nhB),
			V2HashEqual:   nhA == nhB,
		})
	}
	return rows, nil
}

func equalSnap(x, y []int32) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// firstTrueDiffTick 返回快照首次不一致的帧号；全程一致返回 -1。
func firstTrueDiffTick(rows []FrameRow) int {
	for _, r := range rows {
		if !r.SnapshotEqual {
			return r.Tick
		}
	}
	return -1
}

// firstJudgedTick 返回窄位哈希首次判定分叉的帧号；全程“一致”返回 -1。
func firstJudgedTick(rows []FrameRow, useV2 bool) int {
	for _, r := range rows {
		if useV2 {
			if !r.V2HashEqual {
				return r.Tick
			}
		} else if !r.HashEqual {
			return r.Tick
		}
	}
	return -1
}

// rawToWorld 把 Q16.16 原始值换算成世界单位（定点小数）。
func rawToWorld(deltaRaw int32) float64 {
	return float64(deltaRaw) / float64(fixed.One.Raw())
}
