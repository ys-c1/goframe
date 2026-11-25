package api

import (
	"testing"
	"time"
)

// TestDedupCostEstimate 给出一条可复核的开销估算：默认帧参数下，
// 归并一次 k 条指令与完整推进一帧的耗时比。两种情形各测一遍：
// 无重复（命中快路径，不复制）与全重复（走完整归并，分配输出）。
func TestDedupCostEstimate(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过计时")
	}
	opt := Options{Bodies: 8, SubSteps: 4, Gravity: 2}
	for _, k := range []int{1, 8, 32, 128} {
		uniq := make([]Command, k)
		for i := range uniq {
			uniq[i] = Command{Tick: 0, Body: i % opt.Bodies, PushX: 1 << 12}
		}
		dup := make([]Command, k)
		for i := range dup {
			dup[i] = Command{Tick: 0, Body: 0, PushX: 1 << 12}
		}
		noDup := timeIt(5000, func() { _ = DedupCommands(uniq) })
		allDup := timeIt(5000, func() { _ = DedupCommands(dup) })
		frame := timeIt(5000, func() {
			w, _ := New(opt)
			_ = w.Step(uniq)
		})
		t.Logf("k=%d 无重复=%v 全重复=%v 整帧=%v 倍数=%s", k, noDup, allDup, frame,
			ratio(noDup, frame)+"/"+ratio(allDup, frame))
	}
}

func ratio(x, y time.Duration) string {
	if y == 0 {
		return "-"
	}
	return fmtRatio(float64(x) / float64(y))
}

func fmtRatio(f float64) string {
	// 保留两位小数的百分比表示，避免引入额外依赖。
	whole := int(f)
	frac := int((f-float64(whole))*100 + 0.5)
	return itoa(whole) + "." + twoDigits(frac) + "x"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	s := ""
	for v > 0 {
		s = string(rune('0'+v%10)) + s
		v /= 10
	}
	return s
}

func twoDigits(v int) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}

func timeIt(n int, f func()) time.Duration {
	// 预热一次。
	f()
	start := time.Now()
	for i := 0; i < n; i++ {
		f()
	}
	return time.Since(start) / time.Duration(n)
}
