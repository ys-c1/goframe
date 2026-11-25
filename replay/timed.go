package replay

import (
	"fmt"
	"sort"

	"goframe/api"
	"goframe/internal/bucket"
)

// TimedCommand 是网络收上来的原始指令：不带本地帧号，只带整数时间戳。
// 时间戳单位由部署约定（例如 1 单位 = 1ms），两端必须一致。
type TimedCommand struct {
	Timestamp int64 `json:"timestamp"` // 网络时间戳（整数时间单位）
	Body      int   `json:"body"`      // 目标实体
	PushX     int   `json:"push_x"`    // X 冲量，Q16.16 原始整数
	PushY     int   `json:"push_y"`    // Y 冲量，Q16.16 原始整数
}

// TimedLog 是带时间戳的录制：同一段原始网络指令，帧长与帧数是分桶参数。
type TimedLog struct {
	Options     api.Options    `json:"options"`
	FrameLength int64          `json:"frame_length"` // 帧长 F，时间戳单位
	Ticks       int            `json:"ticks"`        // 回放总帧数
	Commands    []TimedCommand `json:"commands"`
}

// BuildLog 按 mode 把时间戳指令分桶重排为可回放的帧日志。
//
// 同一帧内的指令按 (Timestamp,Body,PushX,PushY) 全序排序，因此输入
// 到达顺序无论怎样被网络重排，产出的 Log 都相同（排列不变性）。
// 越界帧号或非法参数返回错误。
func BuildLog(tl TimedLog, mode bucket.Mode) (Log, error) {
	if !bucket.Valid(tl.FrameLength) {
		return Log{}, fmt.Errorf("%w: FrameLength=%d", bucket.ErrFrameLength, tl.FrameLength)
	}
	if tl.Ticks <= 0 {
		return Log{}, fmt.Errorf("replay: Ticks 必须为正，得到 %d", tl.Ticks)
	}
	if _, err := api.New(tl.Options); err != nil {
		return Log{}, err
	}

	ordered := make([]TimedCommand, len(tl.Commands))
	copy(ordered, tl.Commands)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Timestamp != b.Timestamp {
			return a.Timestamp < b.Timestamp
		}
		if a.Body != b.Body {
			return a.Body < b.Body
		}
		if a.PushX != b.PushX {
			return a.PushX < b.PushX
		}
		return a.PushY < b.PushY
	})

	frames := make([]Frame, tl.Ticks)
	for _, tc := range ordered {
		f := bucket.Frame(tc.Timestamp, tl.FrameLength, mode)
		if f < 0 || f >= int64(tl.Ticks) {
			return Log{}, fmt.Errorf("replay: 时间戳 %d 分到帧 %d，越出 [0,%d)",
				tc.Timestamp, f, tl.Ticks)
		}
		frames[f].Commands = append(frames[f].Commands, api.Command{
			Tick:  int(f),
			Body:  tc.Body,
			PushX: tc.PushX,
			PushY: tc.PushY,
		})
	}
	return Log{Options: tl.Options, Frames: frames}, nil
}

// DirectRun 直接跑一个已分好桶的帧序列，返回每帧指纹。
// 语义上等同于“权威直跑”，供与 BuildLog+Playback 的结果逐帧对拍。
func DirectRun(opt api.Options, frames []Frame) ([]uint64, error) {
	_, hashes, err := Record(opt, frames)
	return hashes, err
}
