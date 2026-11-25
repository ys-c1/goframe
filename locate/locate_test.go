package locate_test

import (
	"math/rand"
	"testing"

	"goframe/api"
	"goframe/locate"
	"goframe/replay"
)

// build 构造 frames 帧、每帧一条冲量指令的确定性日志。
func build(frames, bodies int) []replay.Frame {
	fs := make([]replay.Frame, frames)
	for i := range fs {
		fs[i] = replay.Frame{Commands: []api.Command{
			{Tick: i, Body: i % bodies, PushX: 1 << 14},
		}}
	}
	return fs
}

// run 跑完整条日志，逐帧收集指纹与状态快照。
func run(opt api.Options, frames []replay.Frame) ([]uint64, [][]int32, error) {
	w, err := api.New(opt)
	if err != nil {
		return nil, nil, err
	}
	hashes := make([]uint64, 0, len(frames))
	snaps := make([][]int32, 0, len(frames))
	for _, f := range frames {
		if err := w.Step(f.Commands); err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, w.Hash())
		snaps = append(snaps, append([]int32(nil), w.Snapshot()...))
	}
	return hashes, snaps, nil
}

// mutate 深拷贝日志并把第 at 帧的冲量改为另一个确定值，模拟回放分歧。
func mutate(frames []replay.Frame, at int) []replay.Frame {
	out := make([]replay.Frame, len(frames))
	for i := range frames {
		cmds := make([]api.Command, len(frames[i].Commands))
		copy(cmds, frames[i].Commands)
		out[i] = replay.Frame{Commands: cmds}
	}
	out[at].Commands[0].PushX = 3 << 14
	return out
}

func equalSnap(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestExhaustiveAgreement 枚举每种帧长与每个分歧位置，逐位置断言二分与线性
// 扫描给出的帧号完全一致，并校验二分比较次数不超过对数上界。
func TestExhaustiveAgreement(t *testing.T) {
	const maxN = 200
	rng := rand.New(rand.NewSource(1))
	for N := 1; N <= maxN; N++ {
		// d == -1 表示无分歧；否则第 d 帧起进入分歧后缀。
		for d := -1; d < N; d++ {
			a := make([]uint64, N)
			b := make([]uint64, N)
			for i := 0; i < N; i++ {
				a[i] = rng.Uint64()
				if d >= 0 && i >= d {
					b[i] = rng.Uint64()
				} else {
					b[i] = a[i]
				}
			}
			lin := locate.Linear(a, b)
			bin := locate.Binary(a, b)
			want := d
			if lin.Frame != want || bin.Frame != want {
				t.Fatalf("N=%d d=%d: 帧号不一致 linear=%d binary=%d want=%d",
					N, d, lin.Frame, bin.Frame, want)
			}
			if bin.Comparisons > locate.Bound(N) {
				t.Fatalf("N=%d d=%d: 二分比较 %d 次超过上界 %d",
					N, d, bin.Comparisons, locate.Bound(N))
			}
			if lin.Comparisons > N {
				t.Fatalf("N=%d d=%d: 线性比较 %d 次超过 N", N, d, lin.Comparisons)
			}
		}
	}
}

// TestBoundIsLog 验证 Bound(N) = ceil(log2(N+1))，并核对 100/1000 两档。
func TestBoundIsLog(t *testing.T) {
	for N := 1; N <= 5000; N++ {
		// 用整数移位手算 ceil(log2(N+1))，不引入浮点。
		want, v := 0, uint(N)
		for v > 0 {
			want++
			v >>= 1
		}
		if got := locate.Bound(N); got != want {
			t.Fatalf("Bound(%d)=%d, want %d", N, got, want)
		}
	}
	if locate.Bound(100) != 7 || locate.Bound(1000) != 10 {
		t.Fatalf("100/1000 档上界异常: %d %d", locate.Bound(100), locate.Bound(1000))
	}
	if locate.Bound(0) != 0 {
		t.Fatal("N=0 时上界应为 0")
	}
}

// TestSimDivergence 在真实仿真上验证定理一：首个哈希不同帧恰为首个状态不同
// 帧，且此前所有帧的哈希与状态快照都完全一致；两种定位结果也必须相同。
func TestSimDivergence(t *testing.T) {
	const N = 120
	opt := api.Options{Bodies: 8, SubSteps: 4, Gravity: 2}
	base := build(N, opt.Bodies)

	for _, d := range []int{0, 1, 37, N - 1} {
		ha, sa, err := run(opt, base)
		if err != nil {
			t.Fatal(err)
		}
		hb, sb, err := run(opt, mutate(base, d))
		if err != nil {
			t.Fatal(err)
		}

		lin := locate.Linear(ha, hb)
		bin := locate.Binary(ha, hb)
		if lin.Frame != d || bin.Frame != d {
			t.Fatalf("d=%d: linear=%d binary=%d", d, lin.Frame, bin.Frame)
		}
		// 前缀：哈希与逐字节状态都必须相同。
		for i := 0; i < d; i++ {
			if ha[i] != hb[i] {
				t.Fatalf("d=%d: 第 %d 帧哈希提前分歧", d, i)
			}
			if !equalSnap(sa[i], sb[i]) {
				t.Fatalf("d=%d: 第 %d 帧状态提前分歧", d, i)
			}
		}
		// 分歧帧：哈希不同且状态快照确实不同（引理 1 逆否 + 无碰撞）。
		if ha[d] == hb[d] {
			t.Fatalf("d=%d: 分歧帧哈希却相同", d)
		}
		if equalSnap(sa[d], sb[d]) {
			t.Fatalf("d=%d: 分歧帧状态快照却相同", d)
		}
	}
}

// TestNoDivergence 无分歧时两种定位都应返回 -1，且比较次数都是 N。
func TestNoDivergence(t *testing.T) {
	opt := api.Options{Bodies: 4, SubSteps: 2, Gravity: 1}
	frames := build(50, opt.Bodies)
	ha, _, err := run(opt, frames)
	if err != nil {
		t.Fatal(err)
	}
	hb, _, err := run(opt, frames)
	if err != nil {
		t.Fatal(err)
	}
	lin := locate.Linear(ha, hb)
	bin := locate.Binary(ha, hb)
	if lin.Frame != -1 || bin.Frame != -1 {
		t.Fatalf("无分歧却报分歧: linear=%d binary=%d", lin.Frame, bin.Frame)
	}
	if lin.Comparisons != len(ha) || bin.Comparisons > locate.Bound(len(ha)) {
		t.Fatalf("无分歧比较次数异常: linear=%d binary=%d N=%d",
			lin.Comparisons, bin.Comparisons, len(ha))
	}
}

// worstCases 枚举所有分歧位置，返回两种定位的最坏比较次数与平均比较次数。
func worstCases(N int) (linWorst, binWorst, linAvg, binAvg int) {
	base := make([]uint64, N)
	for i := range base {
		base[i] = uint64(i)*1000003 + 7
	}
	linSum, binSum := 0, 0
	for d := -1; d < N; d++ {
		b := append([]uint64(nil), base...)
		if d >= 0 {
			for i := d; i < N; i++ {
				b[i] ^= 0xdeadbeef
			}
		}
		lin := locate.Linear(base, b)
		bin := locate.Binary(base, b)
		if lin.Comparisons > linWorst {
			linWorst = lin.Comparisons
		}
		if bin.Comparisons > binWorst {
			binWorst = bin.Comparisons
		}
		linSum += lin.Comparisons
		binSum += bin.Comparisons
	}
	cases := N + 1
	linAvg = (linSum + cases/2) / cases
	binAvg = (binSum + cases/2) / cases
	return
}

// TestComparisonRatios 帧数取 100 与 1000 两档，实测最坏比较次数之比应对照
// 线性 N 与对数上界 Bound(N) 之比，相对偏差不超过 10%。
func TestComparisonRatios(t *testing.T) {
	for _, N := range []int{100, 1000} {
		linWorst, binWorst, linAvg, binAvg := worstCases(N)
		if binWorst != locate.Bound(N) {
			t.Fatalf("N=%d: 二分最坏 %d 次，期望达到上界 %d", N, binWorst, locate.Bound(N))
		}
		if linWorst != N {
			t.Fatalf("N=%d: 线性最坏应为 %d 次，实际 %d", N, N, linWorst)
		}
		got := float64(linWorst) / float64(binWorst)
		want := float64(N) / float64(locate.Bound(N))
		if rel := (got - want) / want; rel < -0.10 || rel > 0.10 {
			t.Fatalf("N=%d: 比值 %.3f 偏离对数/线性之比 %.3f 超过 10%%", N, got, want)
		}
		t.Logf("N=%4d 线性最坏=%4d 二分最坏=%2d 实测比=%6.2f 理论比=%6.2f "+
			"线性均值=%4d 二分均值=%2d",
			N, linWorst, binWorst, got, want, linAvg, binAvg)
	}
}
