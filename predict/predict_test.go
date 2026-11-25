package predict

import (
	"testing"

	"goframe/internal/fixed"
	"goframe/internal/sim"
)

func testConfig(lead, replay int) Config {
	return Config{
		Bodies:       1,
		Lead:         lead,
		Replay:       replay,
		InitialVelX:  fixed.FromRaw(4096),
		ServerNudgeV: fixed.FromRaw(1),
	}
}

// TestRingRetention 验证环形缓冲恰好保留 [t-R, t] 共 R+1 帧，更老的帧淘汰。
func TestRingRetention(t *testing.T) {
	r := NewRing(3) // R=3 → 4 个槽
	st := func(tick int) *sim.State {
		s := sim.NewState(1, fixed.FromInt(0))
		s.Push(0, fixed.FromRaw(tick), 0)
		return s
	}
	for tick := 1; tick <= 10; tick++ {
		r.Put(tick, st(tick))
	}
	if got := r.Get(7); got == nil || got.VelX(0).Raw() != 7 {
		t.Fatalf("帧 7 应仍在 R=3 的缓冲内（覆盖 7..10）")
	}
	if got := r.Get(6); got != nil {
		t.Fatalf("帧 6 应已被淘汰，Get 返回 %v", got)
	}
	if r.Oldest() != 7 {
		t.Fatalf("最老帧应为 7，实际 %d", r.Oldest())
	}
}

// TestConvergenceBoundary 逐档验证：R>=N 平滑回放且亚像素；R<N 只能硬对齐。
func TestConvergenceBoundary(t *testing.T) {
	cases := []struct {
		lead, replay int
		wantReplay   bool // 是否全部走回放纠正
		wantMaxJump  float64
	}{
		{1, 1, true, 0.001},
		{3, 1, false, 5.0},  // 条件不满足：硬对齐，跳跃约 6px
		{3, 3, true, 0.002},
		{5, 1, false, 9.0},  // 条件不满足：硬对齐，跳跃约 10px
		{5, 5, true, 0.003},
	}
	for _, tc := range cases {
		cfg := testConfig(tc.lead, tc.replay)
		srv := NewServer(cfg)
		cli := NewClient(cfg)
		mailbox := map[int]Authority{}
		const ticks = 120
		for tick := 1; tick <= ticks; tick++ {
			a := srv.Step()
			mailbox[tick+cfg.Lead] = a
			cli.Predict()
			if m, ok := mailbox[tick]; ok {
				_, mode, err := cli.Reconcile(m)
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantReplay && mode != "replay" {
					t.Fatalf("N=%d R=%d: 期望回放纠正，实际 %s", tc.lead, tc.replay, mode)
				}
				if !tc.wantReplay && mode != "resync" {
					t.Fatalf("N=%d R=%d: 期望硬对齐，实际 %s", tc.lead, tc.replay, mode)
				}
				delete(mailbox, tick)
			}
			cli.ObserveGap(Authority{Tick: srv.Tick(), State: srv.State()})
		}
		jump := cli.MaxSnapPx()
		if cli.MaxResyncPx() > jump {
			jump = cli.MaxResyncPx()
		}
		if tc.wantReplay {
			if jump >= 1.0 {
				t.Fatalf("N=%d R=%d: 回放跳跃应 <1px，实际 %.4fpx", tc.lead, tc.replay, jump)
			}
			if cli.Resyncs() != 0 {
				t.Fatalf("N=%d R=%d: 不应出现硬对齐，实际 %d 次", tc.lead, tc.replay, cli.Resyncs())
			}
		} else if jump < tc.wantMaxJump {
			t.Fatalf("N=%d R=%d: 硬对齐跳跃应 >=%.1fpx，实际 %.4fpx", tc.lead, tc.replay, tc.wantMaxJump, jump)
		}
	}
}

// TestReplayJumpScalesWithLead 验证回放跳跃随提前量线性放大（同量级修正下）。
func TestReplayJumpScalesWithLead(t *testing.T) {
	prev := 0.0
	for _, n := range []int{1, 3, 5} {
		cfg := testConfig(n, n)
		srv := NewServer(cfg)
		cli := NewClient(cfg)
		mailbox := map[int]Authority{}
		for tick := 1; tick <= 60; tick++ {
			a := srv.Step()
			mailbox[tick+n] = a
			cli.Predict()
			if m, ok := mailbox[tick]; ok {
				cli.Reconcile(m)
				delete(mailbox, tick)
			}
		}
		jump := cli.MaxSnapPx()
		if jump <= prev {
			t.Fatalf("提前量 %d 的跳跃 %.6fpx 未大于前档 %.6fpx", n, jump, prev)
		}
		if jump >= 1.0 {
			t.Fatalf("提前量 %d 的回放跳跃 %.6fpx 超过 1px", n, jump)
		}
		prev = jump
	}
}
