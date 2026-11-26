package rollback

import "testing"

func TestRingKeepsFiveAndPopsLIFO(t *testing.T) {
	r := NewFive()
	if r.Capacity() != 5 {
		t.Fatalf("capacity=%d", r.Capacity())
	}
	for i := 0; i < 7; i++ {
		r.Push(Blob{TickBefore: i, Codec: []byte{byte(i), byte(i + 1)}})
	}
	// 7 次写入只保留 5 帧，最旧的 tick 0、1 被挤出。
	if r.Len() != 5 {
		t.Fatalf("len=%d, 应为 5", r.Len())
	}
	wantTick := 6
	for r.Len() > 0 {
		b, ok := r.Pop()
		if !ok || b.TickBefore != wantTick {
			t.Fatalf("LIFO 顺序错误: got tick=%d ok=%v, want %d", b.TickBefore, ok, wantTick)
		}
		wantTick--
	}
	if _, ok := r.Pop(); ok {
		t.Fatal("空环 Pop 应返回 false")
	}
}

func TestRingBytesCountsCodecOnly(t *testing.T) {
	r := NewFive()
	r.Push(Blob{TickBefore: 0, Codec: make([]byte, 18)})
	r.Push(Blob{TickBefore: 1, Codec: make([]byte, 30)})
	if r.Bytes() != 48 {
		t.Fatalf("Bytes=%d, 应为 48", r.Bytes())
	}
	// Push 后修改原切片不应影响环内副本。
	blob := []byte{1, 2, 3}
	r.Push(Blob{TickBefore: 2, Codec: blob})
	blob[0] = 99
	all := r.All()
	last := all[len(all)-1]
	if last.Codec[0] != 1 {
		t.Fatal("Ring 必须复制 Codec，避免别名篡改")
	}
}
