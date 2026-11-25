package replay_test

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"

	"goframe/api"
	"goframe/internal/bucket"
	"goframe/replay"
)

// 与 cmd/framelab 共用的同一段跨边界录制：
// 12 / 40 / 112 分别是 F=8/16/32 的半帧精确平局点 c_1/c_2/c_3。
func baseTimedLog(F int64) replay.TimedLog {
	return replay.TimedLog{
		Options:     api.Options{Bodies: 2, SubSteps: 1, Gravity: 0},
		FrameLength: F,
		Ticks:       16,
		Commands: []replay.TimedCommand{
			{Timestamp: 12, Body: 0, PushX: 1 << 14},
			{Timestamp: 40, Body: 0, PushX: 1 << 14},
			{Timestamp: 112, Body: 0, PushX: 1 << 14},
		},
	}
}

// TestFloorReplayEqualsDirect 是核心验收：三档帧长下，floor 分桶后的
// “录制 → JSON 落盘 → 重载 → 回放”必须与直跑逐帧哈希相同。
func TestFloorReplayEqualsDirect(t *testing.T) {
	for _, F := range []int64{8, 16, 32} {
		tl := baseTimedLog(F)

		// 直跑：floor 分桶构建出的权威帧序列直接跑。
		directLog, err := replay.BuildLog(tl, bucket.Floor)
		if err != nil {
			t.Fatalf("F=%d BuildLog: %v", F, err)
		}
		direct, err := replay.DirectRun(directLog.Options, directLog.Frames)
		if err != nil {
			t.Fatalf("F=%d DirectRun: %v", F, err)
		}

		// 录制序列化再反序列化，模拟落盘与网络回放。
		raw, err := json.Marshal(tl)
		if err != nil {
			t.Fatal(err)
		}
		var reloaded replay.TimedLog
		if err := json.Unmarshal(raw, &reloaded); err != nil {
			t.Fatal(err)
		}
		replayLog, err := replay.BuildLog(reloaded, bucket.Floor)
		if err != nil {
			t.Fatalf("F=%d replay BuildLog: %v", F, err)
		}
		played, err := replay.Playback(replayLog)
		if err != nil {
			t.Fatalf("F=%d Playback: %v", F, err)
		}

		if len(played) != len(direct) {
			t.Fatalf("F=%d 帧数不一致 %d vs %d", F, len(played), len(direct))
		}
		for i := range direct {
			if played[i] != direct[i] {
				t.Fatalf("F=%d tick=%d 哈希不一致 direct=%016x replay=%016x",
					F, i, direct[i], played[i])
			}
		}
	}
}

// TestFloorShuffleInvariant 验证唯一性的操作含义：网络到达顺序被随机
// 重排后，floor 分桶回放的逐帧哈希必须与未重排时完全相同。
func TestFloorShuffleInvariant(t *testing.T) {
	for _, F := range []int64{8, 16, 32} {
		tl := baseTimedLog(F)
		wantLog, err := replay.BuildLog(tl, bucket.Floor)
		if err != nil {
			t.Fatal(err)
		}
		want, err := replay.Playback(wantLog)
		if err != nil {
			t.Fatal(err)
		}

		rng := rand.New(rand.NewSource(20261126 + F))
		for trial := 0; trial < 10; trial++ {
			shuffled := append([]replay.TimedCommand(nil), tl.Commands...)
			rng.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})
			stl := tl
			stl.Commands = shuffled
			gotLog, err := replay.BuildLog(stl, bucket.Floor)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotLog.Frames, wantLog.Frames) {
				t.Fatalf("F=%d trial=%d 重排后帧内容改变", F, trial)
			}
			got, err := replay.Playback(gotLog)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("F=%d trial=%d 重排后哈希改变", F, trial)
			}
		}
	}
}

// TestRoundAdjacentBucketsDiverge 是反面对照：把跨边界指令放在相邻两帧，
// 两版回放必须确实分叉，且分叉点就是较早的候选帧。
func TestRoundAdjacentBucketsDiverge(t *testing.T) {
	cases := []struct {
		F    int64
		ts   int64
		want int // 较早帧 k
	}{
		{8, 12, 1},
		{16, 40, 2},
		{32, 112, 3},
	}
	for _, c := range cases {
		tl := baseTimedLog(c.F)
		lo := adjacentLog(t, tl, c.ts, c.want)
		hi := adjacentLog(t, tl, c.ts, c.want+1)
		hl, err := replay.Playback(lo)
		if err != nil {
			t.Fatal(err)
		}
		hh, err := replay.Playback(hi)
		if err != nil {
			t.Fatal(err)
		}
		d := firstDiff(hl, hh)
		if d != c.want {
			t.Fatalf("F=%d 分叉帧=%d, want %d", c.F, d, c.want)
		}
	}
}

func adjacentLog(t *testing.T, tl replay.TimedLog, moveTS int64, targetFrame int) replay.Log {
	t.Helper()
	l, err := replay.BuildLog(tl, bucket.Floor)
	if err != nil {
		t.Fatal(err)
	}
	// 找到该时间戳对应的指令，从 floor 桶取出，强制放进 targetFrame。
	src := bucket.FloorIndex(moveTS, tl.FrameLength)
	for i, c := range l.Frames[src].Commands {
		_ = i
		if c.Tick == int(src) && c.Body == 0 && c.PushX == 1<<14 {
			cmd := c
			cmd.Tick = targetFrame
			l.Frames[src].Commands = removeCmd(l.Frames[src].Commands, i)
			l.Frames[targetFrame].Commands = append(l.Frames[targetFrame].Commands, cmd)
			return l
		}
	}
	t.Fatalf("F=%d 找不到 ts=%d 的指令在 floor 帧 %d", tl.FrameLength, moveTS, src)
	return l
}

func removeCmd(cs []api.Command, idx int) []api.Command {
	out := append([]api.Command(nil), cs...)
	return append(out[:idx], out[idx+1:]...)
}

func firstDiff(a, b []uint64) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}
