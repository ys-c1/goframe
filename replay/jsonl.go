package replay

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"goframe/api"
)

// LogKind 是日志文件首行的标识。
const LogKind = "goframe-replay-log"

// 日志格式版本：
//   - FormatV1：指令分量存 Q16.16 raw 全精度整数
//   - FormatV2：指令分量存量化档位值 level，raw = level*StepRaw
const (
	FormatV1 = 1
	FormatV2 = 2
)

type jsonHeader struct {
	Kind      string `json:"kind"`
	Format    int    `json:"format"`
	Frames    int    `json:"frames"`
	Bodies    int    `json:"bodies"`
	SubSteps  int    `json:"substeps"`
	Gravity   int    `json:"gravity"`
	QuantBits int    `json:"quantBits,omitempty"`
	StepRaw   int    `json:"stepRaw,omitempty"`
}

type jsonFrame struct {
	Tick int      `json:"tick"`
	Cmds [][3]int `json:"cmds"`
}

// WriteJSONL 写 v1 全精度日志，返回写入字节数。
func WriteJSONL(w io.Writer, opt api.Options, frames []Frame) (int, error) {
	return writeJSONL(w, jsonHeader{
		Kind:     LogKind,
		Format:   FormatV1,
		Frames:   len(frames),
		Bodies:   opt.Bodies,
		SubSteps: opt.SubSteps,
		Gravity:  opt.Gravity,
	}, frames, 0)
}

// WriteQuantizedJSONL 写 v2 量化日志。frames 必须已经按 stepRaw 量化，
// 每个分量都应是 stepRaw 的整数倍；落盘时改存档位值以缩小体积。
func WriteQuantizedJSONL(w io.Writer, opt api.Options, frames []Frame, bits, stepRaw int) (int, error) {
	if stepRaw <= 0 {
		return 0, fmt.Errorf("stepRaw 必须为正: %d", stepRaw)
	}
	return writeJSONL(w, jsonHeader{
		Kind:      LogKind,
		Format:    FormatV2,
		Frames:    len(frames),
		Bodies:    opt.Bodies,
		SubSteps:  opt.SubSteps,
		Gravity:   opt.Gravity,
		QuantBits: bits,
		StepRaw:   stepRaw,
	}, frames, stepRaw)
}

func writeJSONL(w io.Writer, h jsonHeader, frames []Frame, stepRaw int) (int, error) {
	cw := &countingWriter{w: w}
	bw := bufio.NewWriter(cw)
	enc := json.NewEncoder(bw)
	if err := enc.Encode(h); err != nil {
		return cw.n, err
	}
	for i, f := range frames {
		jf := jsonFrame{Tick: i, Cmds: make([][3]int, 0, len(f.Commands))}
		for _, c := range f.Commands {
			x, y := c.PushX, c.PushY
			if stepRaw > 0 {
				if x%stepRaw != 0 || y%stepRaw != 0 {
					return cw.n, fmt.Errorf("tick=%d body=%d 存在未按步长 %d 量化的分量", i, c.Body, stepRaw)
				}
				x /= stepRaw
				y /= stepRaw
			}
			jf.Cmds = append(jf.Cmds, [3]int{c.Body, x, y})
		}
		if err := enc.Encode(jf); err != nil {
			return cw.n, err
		}
	}
	if err := bw.Flush(); err != nil {
		return cw.n, err
	}
	return cw.n, nil
}

// countingWriter 统计写入字节数，同时写到底层 writer。
type countingWriter struct {
	w io.Writer
	n int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	k, err := c.w.Write(p)
	c.n += k
	return k, err
}

// ReadJSONL 读回 v1/v2 日志；v2 的档位值会乘回 StepRaw，得到 Q16.16 raw 指令。
func ReadJSONL(r io.Reader) (Log, int, int, error) {
	br := bufio.NewReader(r)
	dec := json.NewDecoder(br)
	var h jsonHeader
	if err := dec.Decode(&h); err != nil {
		return Log{}, 0, 0, fmt.Errorf("读日志头失败: %w", err)
	}
	if h.Kind != LogKind {
		return Log{}, 0, 0, fmt.Errorf("非法日志标识: %q", h.Kind)
	}
	opt := api.Options{Bodies: h.Bodies, SubSteps: h.SubSteps, Gravity: h.Gravity}
	log := Log{Options: opt, Frames: make([]Frame, 0, h.Frames)}
	for {
		var jf jsonFrame
		if err := dec.Decode(&jf); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return Log{}, 0, 0, fmt.Errorf("读帧失败: %w", err)
		}
		f := Frame{Commands: make([]api.Command, 0, len(jf.Cmds))}
		for _, t := range jf.Cmds {
			body, x, y := t[0], t[1], t[2]
			if h.Format == FormatV2 {
				x *= h.StepRaw
				y *= h.StepRaw
			}
			f.Commands = append(f.Commands, api.Command{Tick: jf.Tick, Body: body, PushX: x, PushY: y})
		}
		log.Frames = append(log.Frames, f)
	}
	return log, h.Format, h.QuantBits, nil
}
