package replay

import (
	"testing"

	"goframe/api"
)

// TestPlaybackReproducesPerturbation 开启随机扰动后，
// Record 得到的指纹必须与 Playback 逐项一致（种子随 Options 记录）。
func TestPlaybackReproducesPerturbation(t *testing.T) {
	opt := api.Options{
		Bodies: 12, SubSteps: 2, Gravity: 4,
		Seed: 987654321, PerturbAmp: 1 << 15,
	}
	frames := make([]Frame, 24)
	for i := range frames {
		frames[i] = Frame{Commands: []api.Command{
			{Tick: i, Body: i % 12, PushX: 1 << 14},
		}}
	}
	log, recorded, err := Record(opt, frames)
	if err != nil {
		t.Fatal(err)
	}
	played, err := Playback(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) != len(played) {
		t.Fatalf("指纹长度不一致: %d vs %d", len(recorded), len(played))
	}
	for i := range recorded {
		if recorded[i] != played[i] {
			t.Fatalf("回放扰动在 tick=%d 分叉: %016x vs %016x", i, recorded[i], played[i])
		}
	}
}
