package api

import "testing"

// TestQuantizeBitsOption 验证内核侧 QuantizeBits 开关确实改变状态，
// 且 QuantizeBits=8 与手工把指令舍入到 256 网格后再 Step 完全等价。
func TestQuantizeBitsOption(t *testing.T) {
	cmds := []Command{
		{Tick: 0, Body: 0, PushX: 16400, PushY: -8300}, // 非 256 整倍数
		{Tick: 1, Body: 1, PushX: 12345, PushY: 6789},
	}

	full, err := New(Options{Bodies: 2, SubSteps: 2})
	if err != nil {
		t.Fatal(err)
	}
	q8, err := New(Options{Bodies: 2, SubSteps: 2, QuantizeBits: 8})
	if err != nil {
		t.Fatal(err)
	}
	for f := 0; f < 6; f++ {
		if err := full.Step(cmds); err != nil {
			t.Fatal(err)
		}
		if err := q8.Step(cmds); err != nil {
			t.Fatal(err)
		}
	}
	if full.Hash() == q8.Hash() {
		t.Fatal("量化开关未产生可观察的状态差异")
	}

	// 手工预量化（Q=256，半格远离零）：16400 -> 16384, -8300 -> -8192?
	// -8300/256=-32.42 -> 最近 -32*256=-8192（误差 +108）。
	pre := []Command{
		{Tick: 0, Body: 0, PushX: 16384, PushY: -8192},
		{Tick: 1, Body: 1, PushX: 12288, PushY: 6656}, // 12345 -> 48*256=12288; 6789 -> 27*256=6912? 见下
	}
	// 6789/256=26.52 -> 27*256=6912。
	pre[1].PushY = 6912
	manual, err := New(Options{Bodies: 2, SubSteps: 2})
	if err != nil {
		t.Fatal(err)
	}
	for f := 0; f < 6; f++ {
		if err := manual.Step(pre); err != nil {
			t.Fatal(err)
		}
	}
	if manual.Hash() != q8.Hash() {
		t.Fatalf("内核 QuantizeBits=8 与预量化不一致: %016x vs %016x", q8.Hash(), manual.Hash())
	}
}

func TestQuantizeBitsRejectsOutOfRange(t *testing.T) {
	if _, err := New(Options{Bodies: 1, QuantizeBits: 17}); err == nil {
		t.Fatal("QuantizeBits=17 应报错")
	}
	if _, err := New(Options{Bodies: 1, QuantizeBits: -1}); err == nil {
		t.Fatal("QuantizeBits=-1 应报错")
	}
}
