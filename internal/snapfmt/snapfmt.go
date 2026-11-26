// Package snapfmt 实现快照的两种序列化与四种状态块解释方式。
//
// v1（旧版）：大端，且把 N 个状态字段整段反转后落盘。
// v2（规范）：小端，字段顺序与 sim.State.Raw() 的规范交错序一致。
//
// 旧文件在新读取路径上若按「小端 + 不反转」解释（naive），
// 第 k 个字段得到 n_k = bswap32(c_{N-1-k})，这正是漂移的根因。
package snapfmt

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const Magic = "GFSN"

// HeaderSize 是魔数/版本/参数/帧号/字段数/哈希的固定头长度。
const HeaderSize = 40

// Snap 是一份快照的逻辑内容，Raw 始终是规范交错序。
type Snap struct {
	Version  int
	Bodies   int
	SubSteps int
	Gravity  int
	Tick     int
	Hash     uint64
	Raw      []int32
}

var (
	ErrShort = errors.New("快照长度不足")
	ErrMagic = errors.New("魔数不匹配")
	ErrField = errors.New("字段数与实体数不符")
)

func checkBase(b []byte) error {
	if len(b) < HeaderSize {
		return fmt.Errorf("%w: %d < %d", ErrShort, len(b), HeaderSize)
	}
	if string(b[:4]) != Magic {
		return fmt.Errorf("%w: %q", ErrMagic, b[:4])
	}
	return nil
}

// EncodeCanonicalV2 编码规范序 + 小端快照，字节布局与 artifacts 中 v2 一致。
func EncodeCanonicalV2(s Snap) []byte {
	N := 4 * s.Bodies
	b := make([]byte, HeaderSize+4*N)
	copy(b[0:4], Magic)
	binary.LittleEndian.PutUint32(b[4:8], 2)
	binary.LittleEndian.PutUint32(b[8:12], uint32(int32(s.Bodies)))
	binary.LittleEndian.PutUint32(b[12:16], uint32(int32(s.SubSteps)))
	binary.LittleEndian.PutUint32(b[16:20], uint32(int32(s.Gravity)))
	binary.LittleEndian.PutUint32(b[20:24], uint32(int32(s.Tick)))
	binary.LittleEndian.PutUint32(b[24:28], 0) // reserved
	binary.LittleEndian.PutUint32(b[28:32], uint32(N))
	binary.LittleEndian.PutUint64(b[32:40], s.Hash)
	for i, v := range s.Raw {
		binary.LittleEndian.PutUint32(b[HeaderSize+4*i:], uint32(v))
	}
	return b
}

// EncodeLegacyV1 编码旧版快照：大端 + 整段字段反转。
func EncodeLegacyV1(s Snap) []byte {
	N := 4 * s.Bodies
	b := make([]byte, HeaderSize+4*N)
	copy(b[0:4], Magic)
	// 旧头：版本字节 1，字节序标记 1（BE），字段序标记 1（reversed），保留 0。
	b[4], b[5], b[6], b[7] = 1, 1, 1, 0
	put := func(off int, v int) { binary.BigEndian.PutUint32(b[off:off+4], uint32(int32(v))) }
	put(8, s.Bodies)
	put(12, s.SubSteps)
	put(16, s.Gravity)
	put(20, 0) // 旧头保留字位置
	put(24, s.Tick)
	put(28, N)
	binary.BigEndian.PutUint64(b[32:40], s.Hash)
	// 整段反转后以大端写入。
	for k := 0; k < N; k++ {
		v := s.Raw[N-1-k]
		binary.BigEndian.PutUint32(b[HeaderSize+4*k:], uint32(v))
	}
	return b
}

// DecodeCanonicalV2 读取规范 v2。
func DecodeCanonicalV2(b []byte) (Snap, error) {
	if err := checkBase(b); err != nil {
		return Snap{}, err
	}
	if ver := binary.LittleEndian.Uint32(b[4:8]); ver != 2 {
		return Snap{}, fmt.Errorf("不是 v2: version=%d", ver)
	}
	le := binary.LittleEndian
	s := Snap{
		Version:  2,
		Bodies:   int(int32(le.Uint32(b[8:12]))),
		SubSteps: int(int32(le.Uint32(b[12:16]))),
		Gravity:  int(int32(le.Uint32(b[16:20]))),
		Tick:     int(int32(le.Uint32(b[20:24]))),
		Hash:     le.Uint64(b[32:40]),
	}
	N := int(int32(le.Uint32(b[28:32])))
	if N != 4*s.Bodies {
		return Snap{}, fmt.Errorf("%w: N=%d B=%d", ErrField, N, s.Bodies)
	}
	if len(b) != HeaderSize+4*N {
		return Snap{}, fmt.Errorf("%w: len=%d want=%d", ErrShort, len(b), HeaderSize+4*N)
	}
	s.Raw = make([]int32, N)
	for i := range s.Raw {
		s.Raw[i] = int32(le.Uint32(b[HeaderSize+4*i:]))
	}
	return s, nil
}

// DecodeLegacyV1Header 只解析旧头（参数/帧号/头哈希）。
func DecodeLegacyV1Header(b []byte) (Snap, error) {
	if err := checkBase(b); err != nil {
		return Snap{}, err
	}
	if b[4] != 1 {
		return Snap{}, fmt.Errorf("不是 v1: version byte=%d", b[4])
	}
	be := binary.BigEndian
	s := Snap{Version: 1}
	s.Bodies = int(int32(be.Uint32(b[8:12])))
	s.SubSteps = int(int32(be.Uint32(b[12:16])))
	s.Gravity = int(int32(be.Uint32(b[16:20])))
	s.Tick = int(int32(be.Uint32(b[24:28])))
	s.Hash = be.Uint64(b[32:40])
	N := int(int32(be.Uint32(b[28:32])))
	if N != 4*s.Bodies {
		return Snap{}, fmt.Errorf("%w: N=%d B=%d", ErrField, N, s.Bodies)
	}
	if len(b) != HeaderSize+4*N {
		return Snap{}, fmt.Errorf("%w: len=%d want=%d", ErrShort, len(b), HeaderSize+4*N)
	}
	return s, nil
}

// InterpretState 按指定字节序与是否反转来解释快照状态块，返回逻辑 raw 字段。
// reverse=true 表示读取时把字段序再翻回来（旧文件必需）。
func InterpretState(block []byte, order binary.ByteOrder, reverse bool) []int32 {
	N := len(block) / 4
	words := make([]int32, N)
	for i := 0; i < N; i++ {
		words[i] = int32(order.Uint32(block[4*i:]))
	}
	if reverse {
		for i, j := 0, N-1; i < j; i, j = i+1, j-1 {
			words[i], words[j] = words[j], words[i]
		}
	}
	return words
}

// LegacyBlock 取出 v1 文件的状态块。
func LegacyBlock(b []byte) []byte { return b[HeaderSize:] }

// DecodeLegacyV1Normalized 是迁移后的正确读法：大端 + 反转复位。
func DecodeLegacyV1Normalized(b []byte) (Snap, error) {
	s, err := DecodeLegacyV1Header(b)
	if err != nil {
		return Snap{}, err
	}
	s.Raw = InterpretState(LegacyBlock(b), binary.BigEndian, true)
	return s, nil
}

// DecodeNormalized 是 DecodeLegacyV1Normalized 的语义别名：
// 按 v1 标志用大端并反转字段，把旧序快照规范化回 sim.State.Raw() 顺序。
func DecodeNormalized(b []byte) (Snap, error) { return DecodeLegacyV1Normalized(b) }

// DecodeLegacyV1Naive 模拟未迁移的新路径：小端 + 不反转（漂移复现）。
func DecodeLegacyV1Naive(b []byte) (Snap, error) {
	s, err := DecodeLegacyV1Header(b)
	if err != nil {
		return Snap{}, err
	}
	s.Raw = InterpretState(LegacyBlock(b), binary.LittleEndian, false)
	return s, nil
}
