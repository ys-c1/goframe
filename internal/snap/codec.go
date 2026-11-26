package snap

import (
	"encoding/binary"
	"fmt"
)

var endian = binary.LittleEndian

// MarshalBinary 编码全量快照，长度恒为 FullSize(N)。
func (s *Snapshot) MarshalBinary() ([]byte, error) {
	if s.N > MaxFieldID+1 {
		return nil, fmt.Errorf("%w: N=%d 超出 uint16", ErrLength, s.N)
	}
	if len(s.Values) != s.N {
		return nil, fmt.Errorf("%w: snapshot.Values=%d, N=%d", ErrLength, len(s.Values), s.N)
	}
	b := make([]byte, FullSize(s.N))
	copy(b[0:MagicSize], MagicFull)
	endian.PutUint16(b[4:6], uint16(s.N))
	endian.PutUint32(b[6:10], uint32(int32(s.Tick)))
	for i, v := range s.Values {
		off := FullHeaderSize + FieldValueSize*i
		endian.PutUint32(b[off:off+4], uint32(v))
	}
	return b, nil
}

// UnmarshalSnapshot 严格按定长格式解码，拒绝截断、超长与错误魔数。
func UnmarshalSnapshot(b []byte) (*Snapshot, error) {
	if len(b) < FullHeaderSize {
		return nil, fmt.Errorf("全量快照过短: %d 字节", len(b))
	}
	if string(b[0:4]) != MagicFull {
		return nil, fmt.Errorf("全量快照魔数错误: %q", b[0:4])
	}
	n := int(endian.Uint16(b[4:6]))
	if n == 0 {
		return nil, fmt.Errorf("全量快照 N=0")
	}
	if want := FullSize(n); len(b) != want {
		return nil, fmt.Errorf("全量快照长度=%d, 按 N=%d 应为 %d", len(b), n, want)
	}
	s := &Snapshot{
		Kind:     "full",
		Tick:     int(int32(endian.Uint32(b[6:10]))),
		N:        n,
		ByteSize: len(b),
		Values:   make([]int32, n),
	}
	for i := 0; i < n; i++ {
		off := FullHeaderSize + FieldValueSize*i
		s.Values[i] = int32(endian.Uint32(b[off : off+4]))
	}
	return s, nil
}

// MarshalBinary 编码差分，长度恒为 DeltaSize(N,k)=12+6k。
func (d *Delta) MarshalBinary() ([]byte, error) {
	n := d.N
	k := len(d.Entries)
	if n > MaxFieldID+1 {
		return nil, fmt.Errorf("%w: N=%d 超出 uint16", ErrLength, n)
	}
	if k > int(^uint16(0)) {
		return nil, fmt.Errorf("%w: 条目数 %d 超出 uint16", ErrLength, k)
	}
	if err := d.Validate(n); err != nil {
		return nil, err
	}
	b := make([]byte, DeltaSize(n, k))
	copy(b[0:4], MagicDelta)
	endian.PutUint16(b[4:6], uint16(n))
	endian.PutUint32(b[6:10], uint32(int32(d.TickBefore)))
	endian.PutUint16(b[10:12], uint16(k))
	for j, e := range d.Entries {
		off := DeltaHeaderSize + EntrySize*j
		endian.PutUint16(b[off:off+2], uint16(e.ID))
		endian.PutUint32(b[off+2:off+6], uint32(e.Old))
	}
	return b, nil
}

// UnmarshalDelta 严格解码差分并做良构校验（编号越界/重复都会报错）。
func UnmarshalDelta(b []byte) (*Delta, error) {
	if len(b) < DeltaHeaderSize {
		return nil, fmt.Errorf("差分快照过短: %d 字节", len(b))
	}
	if string(b[0:4]) != MagicDelta {
		return nil, fmt.Errorf("差分快照魔数错误: %q", b[0:4])
	}
	n := int(endian.Uint16(b[4:6]))
	tick := int(int32(endian.Uint32(b[6:10])))
	k := int(endian.Uint16(b[10:12]))
	if want := DeltaSize(n, k); len(b) != want {
		return nil, fmt.Errorf("差分长度=%d, 按 N=%d,k=%d 应为 %d", len(b), n, k, want)
	}
	d := &Delta{
		Kind:       "delta",
		TickBefore: tick,
		N:          n,
		ByteSize:   len(b),
		Entries:    make([]Entry, 0, k),
	}
	for j := 0; j < k; j++ {
		off := DeltaHeaderSize + EntrySize*j
		id := FieldID(endian.Uint16(b[off : off+2]))
		old := int32(endian.Uint32(b[off+2 : off+6]))
		d.Entries = append(d.Entries, Entry{ID: id, Old: old})
	}
	if err := d.Validate(n); err != nil {
		return nil, err
	}
	return d, nil
}
