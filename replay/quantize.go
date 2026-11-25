package replay

import (
	"goframe/api"
	"goframe/internal/fixed"
)

// QuantizeFrameToStep 用任意整数 raw 网格步长量化一帧指令，返回新帧，不改原帧。
func QuantizeFrameToStep(f Frame, stepRaw int) Frame {
	out := Frame{Commands: make([]api.Command, len(f.Commands))}
	q := fixed.FromRaw(stepRaw)
	for i, c := range f.Commands {
		c.PushX = int(fixed.QuantizeToStep(fixed.FromRaw(c.PushX), q).Raw())
		c.PushY = int(fixed.QuantizeToStep(fixed.FromRaw(c.PushY), q).Raw())
		out.Commands[i] = c
	}
	return out
}

// QuantizeFrame 按定点小数位 bits（步长 2^-bits）量化一帧指令。
func QuantizeFrame(f Frame, bits int) Frame {
	return QuantizeFrameToStep(f, int(fixed.GridStep(bits)))
}

// QuantizeLogToStep 返回按任意 raw 网格步长量化后的日志副本，Options 不变。
func QuantizeLogToStep(l Log, stepRaw int) Log {
	out := Log{Options: l.Options, Frames: make([]Frame, len(l.Frames))}
	for i, f := range l.Frames {
		out.Frames[i] = QuantizeFrameToStep(f, stepRaw)
	}
	return out
}

// QuantizeLog 返回按定点小数位 bits 量化后的日志副本。
func QuantizeLog(l Log, bits int) Log {
	return QuantizeLogToStep(l, int(fixed.GridStep(bits)))
}

// RunSnapshots 跑一遍日志，返回每帧执行后的状态快照（raw Q16.16）与指纹。
// 快照切片每帧独立，可直接跨运行逐元素比较。
func RunSnapshots(opt api.Options, frames []Frame) ([][]int32, []uint64, error) {
	w, err := api.New(opt)
	if err != nil {
		return nil, nil, err
	}
	snaps := make([][]int32, 0, len(frames))
	hashes := make([]uint64, 0, len(frames))
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			return nil, nil, err
		}
		snaps = append(snaps, w.Snapshot())
		hashes = append(hashes, w.Hash())
	}
	return snaps, hashes, nil
}

// MaxComponentDrift 比较两组等长快照，返回：
//   - posDrift：所有帧、所有位置分量（posX/posY）中的最大绝对差（raw）
//   - allDrift：还包含速度分量的最大绝对差（raw）
//   - finalPosDrift：最后一帧位置分量的最大绝对差（raw）
//
// 快照布局见 sim.State.Raw：每个实体依次 posX,posY,velX,velY。
func MaxComponentDrift(a, b [][]int32) (posDrift, allDrift, finalPosDrift int64) {
	for t := range a {
		var sb []int32
		if t < len(b) {
			sb = b[t]
		}
		for i := range a[t] {
			var vb int32
			if i < len(sb) {
				vb = sb[i]
			}
			d := abs64(int64(a[t][i]) - int64(vb))
			if d > allDrift {
				allDrift = d
			}
			// 每个实体 4 个分量，下标 0、1 是 posX、posY。
			if i%4 == 0 || i%4 == 1 {
				if d > posDrift {
					posDrift = d
				}
				if t == len(a)-1 && d > finalPosDrift {
					finalPosDrift = d
				}
			}
		}
	}
	return posDrift, allDrift, finalPosDrift
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
