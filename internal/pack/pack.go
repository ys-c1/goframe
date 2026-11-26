// Package pack 把 Q16.16 定点量化为 B 个有效高位并按位打包，
// 用紧凑比特流压缩快照体积；还原值保持同一量纲，可直接喂回积分内核。
//
// 量化语义：保留 int32 的高 B 位（算术右移 s=32-B 位后再左移回去），
// 量化步长 Δ=2^s，对任意 int32 输入都有 |x-还原(x)| < Δ。
// 比特流采用 MSB-first 排布，文件头自描述帧结构。
package pack

import (
	"errors"
	"fmt"
)

// Magic 是紧凑快照的魔数。
const Magic = "GFSNAP01"

// headerSize = magic(8) + bits(1) + frames(2, BE) + channels(2, BE)。
const headerSize = 13

var (
	// ErrBadBits 位宽不在 1..32。
	ErrBadBits = errors.New("位宽越界，仅支持 1..32")
	// ErrBadHeader 快照头不合法或帧结构不一致。
	ErrBadHeader = errors.New("快照头不合法")
)

// Quantize 保留 x 的高 bits 位，返回同量纲的还原值（向零取整）。
func Quantize(x int32, bits int) int32 {
	s := uint(32 - bits)
	return (x >> s) << s
}

// Step 返回位宽 bits 对应的量化步长 Δ=2^(32-bits)（raw 单位）。
func Step(bits int) int64 {
	return int64(1) << uint(32-bits)
}

// MaxAbsError 返回先验绝对误差上界（raw 单位）：步长减一。
func MaxAbsError(bits int) int64 {
	return Step(bits) - 1
}

// MinBits 给定 raw 单位的漂移上限 limit，反解满足先验误差上界
// （Δ-1 <= limit）的最小位宽：
//
//	2^(32-B) <= limit+1  =>  B >= 32 - floor(log2(limit+1))
func MinBits(limit int64) int {
	if limit <= 0 {
		return 32
	}
	v := limit + 1
	log := 0
	for v > 1 {
		v >>= 1
		log++
	}
	return 32 - log
}

func encodeCode(x int32, b uint) uint64 {
	s := uint(32) - b
	q := int64(x >> s)
	if b == 32 {
		return uint64(q)
	}
	return uint64(q & (int64(1)<<b - 1))
}

// Pack 把多帧 int32 量化并打包成带魔数与位宽头的紧凑字节流。
func Pack(frames [][]int32, bits int) ([]byte, error) {
	if bits < 1 || bits > 32 {
		return nil, fmt.Errorf("%w: bits=%d", ErrBadBits, bits)
	}
	if len(frames) == 0 || len(frames[0]) == 0 {
		return nil, fmt.Errorf("%w: 空帧", ErrBadHeader)
	}
	ch := len(frames[0])
	for _, f := range frames {
		if len(f) != ch {
			return nil, fmt.Errorf("%w: 通道数不一致", ErrBadHeader)
		}
	}
	if len(frames) > 0xffff || ch > 0xffff {
		return nil, fmt.Errorf("%w: 帧/通道数超头字段", ErrBadHeader)
	}
	b := uint(bits)
	payloadBits := len(frames) * ch * bits
	out := make([]byte, headerSize+(payloadBits+7)/8)
	copy(out[:8], Magic)
	out[8] = byte(bits)
	out[9] = byte(len(frames) >> 8)
	out[10] = byte(len(frames))
	out[11] = byte(ch >> 8)
	out[12] = byte(ch)

	var bitPos uint
	for _, f := range frames {
		for _, x := range f {
			code := encodeCode(x, b)
			for k := b; k > 0; k-- {
				if code&(1<<(k-1)) != 0 {
					out[headerSize+bitPos/8] |= 1 << (7 - bitPos%8)
				}
				bitPos++
			}
		}
	}
	return out, nil
}

// Unpack 读取紧凑字节流，做 B 位补码符号扩展后还原为同量纲 int32。
func Unpack(data []byte) ([][]int32, int, error) {
	if len(data) < headerSize || string(data[:8]) != Magic {
		return nil, 0, ErrBadHeader
	}
	bits := int(data[8])
	if bits < 1 || bits > 32 {
		return nil, 0, ErrBadBits
	}
	frames := int(data[9])<<8 | int(data[10])
	ch := int(data[11])<<8 | int(data[12])
	if frames <= 0 || ch <= 0 {
		return nil, 0, ErrBadHeader
	}
	b := uint(bits)
	s := uint(32) - b
	out := make([][]int32, frames)
	var bitPos uint
	for f := 0; f < frames; f++ {
		out[f] = make([]int32, ch)
		for i := 0; i < ch; i++ {
			var code uint32
			for k := b; k > 0; k-- {
				idx := headerSize + bitPos/8
				if int(idx) >= len(data) {
					return nil, 0, fmt.Errorf("%w: 比特流截断", ErrBadHeader)
				}
				if data[idx]&(1<<(7-bitPos%8)) != 0 {
					code |= 1 << (k - 1)
				}
				bitPos++
			}
			if b < 32 {
				sign := uint32(1) << (b - 1)
				code = (code ^ sign) - sign // B 位补码符号扩展到 32 位
			}
			out[f][i] = int32(code) << s
		}
	}
	return out, bits, nil
}
