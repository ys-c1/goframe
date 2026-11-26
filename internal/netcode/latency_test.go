package netcode

import (
	"testing"

	"goframe/api"
)

func TestTraceDeterministic(t *testing.T) {
	for _, tier := range DefaultTiers() {
		a, err := tier.SampleTrace()
		if err != nil {
			t.Fatal(err)
		}
		b, err := tier.SampleTrace()
		if err != nil {
			t.Fatal(err)
		}
		if len(a) != tier.Frames {
			t.Fatalf("%s 轨迹长度 %d != %d", tier.Name, len(a), tier.Frames)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("%s 轨迹在帧 %d 不可复现: %d vs %d", tier.Name, i, a[i], b[i])
			}
		}
	}
}

func TestEmpiricalQuantiles(t *testing.T) {
	// p99 必须落在分布定义的支撑集内，且不小于 p50。
	for _, tier := range DefaultTiers() {
		trace, _ := tier.SampleTrace()
		p50, p99, max := Quantile(trace, .5), Quantile(trace, .99), 0
		for _, d := range trace {
			if d > max {
				max = d
			}
		}
		if p50 > p99 || p99 > max {
			t.Fatalf("%s 分位反常: p50=%d p99=%d max=%d", tier.Name, p50, p99, max)
		}
		t.Logf("%s p50=%d p99=%d max=%d", tier.Name, p50, p99, max)
	}
}

func TestDepthStarEliminatesOOB(t *testing.T) {
	opt := api.Options{Bodies: 8, SubSteps: 2, Gravity: 1}
	for _, tier := range DefaultTiers() {
		res, err := RunTier(tier, opt)
		if err != nil {
			t.Fatal(err)
		}
		if res.DepthStar.OOB != 0 {
			t.Fatalf("%s D*=%d 仍有 %d 次越界，失败帧=%v",
				tier.Name, res.TheoreticD, res.DepthStar.OOB, res.DepthStar.FailedTicks)
		}
		if res.Depth5.Depth != 5 {
			t.Fatalf("%s 对照深度应为 5", tier.Name)
		}
	}
}

func TestDepth5FailsOnHeavyTails(t *testing.T) {
	opt := api.Options{Bodies: 8, SubSteps: 2, Gravity: 1}
	res, _ := RunTier(DefaultTiers()[2], opt) // wan
	if res.Depth5.OOB == 0 {
		t.Fatal("弱网档 D=5 必须产生越界失败")
	}
	if res.Depth5.FirstOOBTick <= res.Depth5.FirstOOBTarget {
		t.Fatal("到达时刻必须晚于生效帧")
	}
	// 首次失败必须可由轨迹直接复算：at = target + delay。
	d := res.Trace[res.Depth5.FirstOOBTarget]
	if res.Depth5.FirstOOBTick != res.Depth5.FirstOOBTarget+d {
		t.Fatalf("首次失败帧号与轨迹不符: at=%d target=%d traceDelay=%d",
			res.Depth5.FirstOOBTick, res.Depth5.FirstOOBTarget, d)
	}
}

func TestMaxDepthFormula(t *testing.T) {
	bodies := 8
	perFrame := SnapshotBytes(bodies) // 128
	cases := []struct {
		budget, wantD int
	}{
		{512, 3}, {1024, 7}, {2048, 15}, {4096, 31},
	}
	for _, c := range cases {
		if got := MaxDepth(c.budget, bodies); got != c.wantD {
			t.Fatalf("预算 %d: MaxDepth=%d，期望 %d", c.budget, got, c.wantD)
		}
		// 反推占用不得超过预算。
		if (c.wantD+1)*perFrame > c.budget {
			t.Fatalf("深度 %d 占用超出预算 %d", c.wantD, c.budget)
		}
	}
}
