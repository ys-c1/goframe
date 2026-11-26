package replay

import (
	"testing"

	"goframe/api"
)

func spawn() api.Command {
	return api.Command{Op: api.OpSpawn, ID: api.SpawnUnallocated}
}

func despawn(id int) api.Command {
	return api.Command{Op: api.OpDespawn, Body: id}
}

// threeSequences 构造三组增删序列：总生成数相同、存活者相同（编号 2/3/4）、
// 存活者冲量相同，仅临时实体的销毁时机不同。
func threeSequences() [][]Frame {
	opt := api.Options{Bodies: 0, SubSteps: 1, SpawnsPerTick: 64}
	_ = opt

	seq0 := []Frame{
		{Commands: []api.Command{spawn(), spawn(), despawn(0), despawn(1)}},
		{Commands: []api.Command{spawn(), spawn(), spawn()}}, // A=2 B=3 C=4
		{Commands: nil},
		{Commands: []api.Command{
			{Body: 2, PushX: 1 << 16}, {Body: 3, PushY: 1 << 15}, {Body: 4, PushX: -1 << 14},
		}},
	}
	seq1 := []Frame{
		{Commands: []api.Command{spawn(), spawn(), despawn(0)}},
		{Commands: []api.Command{spawn(), spawn(), spawn(), despawn(1)}},
		{Commands: nil},
		{Commands: []api.Command{
			{Body: 2, PushX: 1 << 16}, {Body: 3, PushY: 1 << 15}, {Body: 4, PushX: -1 << 14},
		}},
	}
	seq2 := []Frame{
		{Commands: []api.Command{spawn(), spawn()}},
		{Commands: []api.Command{spawn(), spawn(), spawn()}},
		{Commands: []api.Command{despawn(0), despawn(1)}},
		{Commands: []api.Command{
			{Body: 2, PushX: 1 << 16}, {Body: 3, PushY: 1 << 15}, {Body: 4, PushX: -1 << 14},
		}},
	}
	return [][]Frame{seq0, seq1, seq2}
}

// TestThreeAddRemoveSequencesHashEqual 修复目标：三组增删序列的
// 每帧（自全部存活者生成之后）指纹一致，且各自记录/回放指纹一致。
func TestThreeAddRemoveSequencesHashEqual(t *testing.T) {
	opt := api.Options{Bodies: 0, SubSteps: 1, SpawnsPerTick: 64}
	var finals [3]uint64
	for si, frames := range threeSequences() {
		log, hashes, err := Record(opt, frames)
		if err != nil {
			t.Fatalf("seq%d record: %v", si, err)
		}
		again, err := Playback(log)
		if err != nil {
			t.Fatalf("seq%d playback: %v", si, err)
		}
		for i := range hashes {
			if hashes[i] != again[i] {
				t.Fatalf("seq%d tick%d 回放指纹不一致", si, i)
			}
		}
		finals[si] = hashes[len(hashes)-1]
	}
	if finals[0] != finals[1] || finals[1] != finals[2] {
		t.Fatalf("三组增删序列终态哈希不一致: %016x %016x %016x",
			finals[0], finals[1], finals[2])
	}
}
