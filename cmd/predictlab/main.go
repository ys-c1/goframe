// predictlab 驱动“客户端预测 + 服务器纠正”实验：
// 对三档预测提前量 N∈{1,3,5}，分别跑“重放帧数不足 R=1”与
// “按条件取 R=N”两组配置，输出逐帧轨迹 CSV 与汇总 JSON。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goframe/internal/fixed"
	"goframe/predict"
)

type row struct {
	tick         int
	clientPosRaw int32
	serverPosRaw int32
	clientVelRaw int32
	serverVelRaw int32
	authTick     int
	mode         string // replay / resync / ""（本帧无权威帧到达）
	jumpPx       float64
	gapPx        float64
}

type summary struct {
	Lead        int     `json:"lead"`
	Replay      int     `json:"replay"`
	Condition   bool    `json:"condition_ge_lead"`
	Converged   bool    `json:"converged"`
	Replays     int     `json:"replay_corrections"`
	Resyncs     int     `json:"hard_resyncs"`
	MaxReplayPx float64 `json:"max_replay_jump_px"`
	MaxResyncPx float64 `json:"max_resync_jump_px"`
	MaxJumpPx   float64 `json:"max_jump_px"`
	MaxGapPx    float64 `json:"max_gap_px"`
	FinalGapPx  float64 `json:"final_gap_px"`
}

// gapPxRaw 把 raw 位置差换算成像素（1 世界单位 = 32 像素）。
func gapPxRaw(delta int32) float64 {
	f := float64(delta) / 65536.0 * 32.0
	if f < 0 {
		return -f
	}
	return f
}

func runOne(cfg predict.Config, ticks int) ([]row, summary) {
	srv := predict.NewServer(cfg)
	cli := predict.NewClient(cfg)

	// mailbox[t] 存放应在客户端第 t 帧到达的权威帧（延迟 Lead 帧）。
	mailbox := make(map[int]predict.Authority)

	rows := make([]row, 0, ticks)
	replays := 0
	for t := 1; t <= ticks; t++ {
		a := srv.Step()                // 服务器先推进
		mailbox[t+cfg.Lead] = a        // 延迟投递
		cli.Predict()                  // 客户端本地预测

		jumpPx, mode, authTick := 0.0, "", -1
		if m, ok := mailbox[t]; ok {
			authTick = m.Tick
			j, md, err := cli.Reconcile(m)
			if err != nil {
				panic(err)
			}
			jumpPx, mode = j, md
			if mode == "replay" {
				replays++
			}
			delete(mailbox, t)
		}
		// 与“当前真实权威状态”的渲染偏差
		cur := predict.Authority{Tick: srv.Tick(), State: srv.State()}
		cli.ObserveGap(cur)
		instGap := gapPxRaw(cli.State().PosX(0).Raw() - srv.State().PosX(0).Raw())

		rows = append(rows, row{
			tick:         t,
			clientPosRaw: cli.State().PosX(0).Raw(),
			serverPosRaw: srv.State().PosX(0).Raw(),
			clientVelRaw: cli.State().VelX(0).Raw(),
			serverVelRaw: srv.State().VelX(0).Raw(),
			authTick:     authTick,
			mode:         mode,
			jumpPx:       jumpPx,
			gapPx:        instGap,
		})
	}
	maxJump := cli.MaxSnapPx()
	if cli.MaxResyncPx() > maxJump {
		maxJump = cli.MaxResyncPx()
	}
	sum := summary{
		Lead:        cfg.Lead,
		Replay:      cfg.Replay,
		Condition:   cfg.Replay >= cfg.Lead,
		Replays:     replays,
		Resyncs:     cli.Resyncs(),
		MaxReplayPx: cli.MaxSnapPx(),
		MaxResyncPx: cli.MaxResyncPx(),
		MaxJumpPx:   maxJump,
		MaxGapPx:    cli.MaxGapPx(),
		FinalGapPx:  gapPxRaw(rows[len(rows)-1].clientPosRaw - rows[len(rows)-1].serverPosRaw),
	}
	// 达标判据：全程最大画面跳跃低于 1 像素。
	sum.Converged = sum.MaxJumpPx < 1.0 && sum.Resyncs == 0
	return rows, sum
}

func writeCSV(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, "tick,client_pos_raw,server_pos_raw,client_vel_raw,server_vel_raw,auth_tick,mode,jump_px,gap_px"); err != nil {
		return err
	}
	for _, r := range rows {
		if r.mode == "" {
			r.mode = "none"
		}
		if _, err := fmt.Fprintf(f, "%d,%d,%d,%d,%d,%d,%s,%.6f,%.6f\n",
			r.tick, r.clientPosRaw, r.serverPosRaw, r.clientVelRaw, r.serverVelRaw,
			r.authTick, r.mode, r.jumpPx, r.gapPx); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	outDir := flag.String("out", "artifacts/lab", "输出目录")
	ticks := flag.Int("ticks", 120, "仿真帧数")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	const (
		bodies = 1
		v0     = 4096 // 初始速度 raw/tick（4096/65536*32 = 2 像素/帧）
		nudge  = 1    // 服务器每帧权威速度修正 raw/tick（定点最小有效量级，客户端预测不到）
	)
	leads := []int{1, 3, 5}

	summaries := make([]summary, 0, 2*len(leads))
	for _, n := range leads {
		// 条件不满足：固定只留 1 帧重放（N=1 时恰好满足，作为对照）
		rowsBad, sumBad := runOne(predict.Config{
			Bodies: bodies, Lead: n, Replay: 1,
			InitialVelX: fixed.FromRaw(v0), ServerNudgeV: fixed.FromRaw(nudge),
		}, *ticks)
		if err := writeCSV(filepath.Join(*outDir, fmt.Sprintf("traj_n%d_r1.csv", n)), rowsBad); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		summaries = append(summaries, sumBad)

		// 条件满足：重放帧数取 R=N
		rowsGood, sumGood := runOne(predict.Config{
			Bodies: bodies, Lead: n, Replay: n,
			InitialVelX: fixed.FromRaw(v0), ServerNudgeV: fixed.FromRaw(nudge),
		}, *ticks)
		if err := writeCSV(filepath.Join(*outDir, fmt.Sprintf("traj_n%d_r%d.csv", n, n)), rowsGood); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		summaries = append(summaries, sumGood)
	}

	j, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "summary.json"), j, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(j))
}
