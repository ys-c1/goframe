package api

import (
	"fmt"
)

// Restore 在指定 tick 上用规范交错序 raw 字段重建世界，用于快照回放与对拍。
// raw 顺序必须与 World.Snapshot()/sim.State.Raw() 完全一致。
func Restore(opt Options, tick int, raw []int32) (*World, error) {
	w, err := New(opt)
	if err != nil {
		return nil, err
	}
	if want := 4 * opt.Bodies; len(raw) != want {
		return nil, fmt.Errorf("%w: raw=%d want=%d", ErrBadOption, len(raw), want)
	}
	if err := w.state.SetRaw(raw); err != nil {
		return nil, err
	}
	if tick < 0 {
		return nil, fmt.Errorf("%w: tick=%d", ErrBadOption, tick)
	}
	w.tick = tick
	return w, nil
}
