// Package replay 记录与回放指令序列。
package replay

import "goframe/api"

// Frame 是一帧的指令集合。
type Frame struct {
	Commands []api.Command
}

// Log 是完整回放记录。
type Log struct {
	Options api.Options
	Frames  []Frame
}

// Record 跑一遍并记录，返回日志与每帧指纹。
func Record(opt api.Options, frames []Frame) (Log, []uint64, error) {
	w, err := api.New(opt)
	if err != nil {
		return Log{}, nil, err
	}
	hashes := make([]uint64, 0, len(frames))
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			return Log{}, nil, err
		}
		hashes = append(hashes, w.Hash())
	}
	return Log{Options: opt, Frames: frames}, hashes, nil
}

// Playback 按日志重放并返回指纹。
func Playback(l Log) ([]uint64, error) {
	_, hashes, err := Record(l.Options, l.Frames)
	return hashes, err
}
