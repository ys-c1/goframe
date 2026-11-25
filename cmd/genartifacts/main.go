// Command genartifacts 实跑全部对拍并把证据写入 artifacts/：
//   - replay.json / replay_record.json   一份可回放的记录及 Record/Playback 指纹
//   - substep_invariance.json            1/2/4 + 8 的逐帧哈希矩阵与“差值为零”专检
//   - euler_dependence_proof.json        旧 Euler 子步数相关性与 G*h^2 二阶项数值证明
//   - verification_summary.json          所有硬性检查的 PASS/FAIL 汇总
//
// 任一硬性条件不满足时以非零码退出。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goframe/api"
	"goframe/replay"
	"goframe/verify"
)

type summary struct {
	OK bool `json:"ok"`

	OldEuler1v4HashesDiffer bool `json:"old_euler_sub1_vs_sub4_hashes_differ"`

	Invariance124SameFrame bool `json:"invariance_substeps_1_2_4_same_frame_hashes_identical"`
	Pair1v4AllDeltaZero    bool `json:"pair_sub1_vs_sub4_every_frame_hash_delta_zero"`
	Pair1v4AllXORZero      bool `json:"pair_sub1_vs_sub4_every_frame_hash_xor_zero"`
	Pair1v4RawEqual        bool `json:"pair_sub1_vs_sub4_raw_states_identical"`

	Boundary8Identical bool `json:"boundary_sub8_identical_to_sub1"`

	ReplayIdentical bool `json:"replay_record_playback_identical"`
	ReplayDiskOK    bool `json:"replay_json_saved_and_replayed_from_disk_identical"`

	CanonicalFrames int `json:"canonical_frames"`
	RichFrames      int `json:"rich_frames"`
	Bodies          int `json:"bodies"`
	Gravity         int `json:"gravity"`
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func main() {
	out := flag.String("out", "artifacts", "产物输出目录")
	frames := flag.Int("frames", 20, "规范指令流帧数（与 CLI 演示一致）")
	bodies := flag.Int("bodies", 8, "实体数")
	gravity := flag.Int("gravity", 2, "重力（整数 Q16.16）")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}

	canonical := verify.CanonicalFrames(*frames, *bodies)
	rich := verify.RichFrames(20261126, 32, *bodies)

	// 1) 无关性矩阵：规范流 + 任意原始冲量流。
	canonMatrix, _, err := verify.BuildMatrix(*bodies, *gravity, canonical)
	if err != nil {
		fatal(err)
	}
	richMatrix, _, err := verify.BuildMatrix(*bodies, *gravity, rich)
	if err != nil {
		fatal(err)
	}
	invArtifact := map[string]any{
		"required_levels":             verify.RequiredLevels,
		"boundary_level":              verify.BoundaryLevel,
		"canonical_command_log":       canonMatrix,
		"arbitrary_raw_push_log_note": "非 Q16 对齐、含负冲量与一帧多条指令；2 的幂子步档下仍逐位一致",
		"rich_command_log":            richMatrix,
	}
	if err := writeJSON(filepath.Join(*out, "substep_invariance.json"), invArtifact); err != nil {
		fatal(err)
	}

	// 2) 旧 Euler 相关性 + G*h^2 证明。
	proof, err := verify.BuildEulerProof(*gravity, canonical)
	if err != nil {
		fatal(err)
	}
	if err := writeJSON(filepath.Join(*out, "euler_dependence_proof.json"), proof); err != nil {
		fatal(err)
	}

	// 3) 回放记录（README 场景：20 帧 / 8 体 / sub=4 / g=2）。
	rep, err := verify.BuildReplayArtifact(api.Options{
		Bodies: *bodies, SubSteps: 4, Gravity: *gravity,
	}, canonical)
	if err != nil {
		fatal(err)
	}
	replayPath := filepath.Join(*out, "replay.json")
	for _, name := range []string{"replay.json", "replay_record.json"} {
		if err := writeJSON(filepath.Join(*out, name), rep); err != nil {
			fatal(err)
		}
	}

	// 独立的落盘自检：从磁盘读回 replay.json，解码后再 Playback 一遍。
	diskOK, err := replayFromDiskMatches(replayPath, rep.RecordedHashes)
	if err != nil {
		fatal(err)
	}

	s := summary{
		OldEuler1v4HashesDiffer: proof.CanonicalOldHashesDiffer,
		Invariance124SameFrame:  canonMatrix.SameFrame124 && richMatrix.SameFrame124,
		Pair1v4AllDeltaZero:     canonMatrix.Pair1v4.AllDeltaZero,
		Pair1v4AllXORZero:       canonMatrix.Pair1v4.AllXORZero,
		Pair1v4RawEqual:         canonMatrix.Pair1v4.AllRawEqual,
		Boundary8Identical:      canonMatrix.Boundary8Same && richMatrix.Boundary8Same,
		ReplayIdentical:         rep.ReplayIdentical,
		ReplayDiskOK:            diskOK,
		CanonicalFrames:         *frames,
		RichFrames:              len(rich),
		Bodies:                  *bodies,
		Gravity:                 *gravity,
	}
	s.OK = s.OldEuler1v4HashesDiffer && s.Invariance124SameFrame &&
		s.Pair1v4AllDeltaZero && s.Pair1v4AllXORZero && s.Pair1v4RawEqual &&
		s.Boundary8Identical && s.ReplayIdentical && s.ReplayDiskOK

	if err := writeJSON(filepath.Join(*out, "verification_summary.json"), s); err != nil {
		fatal(err)
	}

	fmt.Printf("old Euler 1v4 hashes differ : %v\n", s.OldEuler1v4HashesDiffer)
	fmt.Printf("midpoint 1/2/4 same-frame   : %v\n", s.Invariance124SameFrame)
	fmt.Printf("pair 1v4 delta/xor/raw zero : %v / %v / %v\n", s.Pair1v4AllDeltaZero, s.Pair1v4AllXORZero, s.Pair1v4RawEqual)
	fmt.Printf("boundary sub=8 identical    : %v\n", s.Boundary8Identical)
	fmt.Printf("replay record==playback     : %v\n", s.ReplayIdentical)
	fmt.Printf("replay.json disk roundtrip  : %v\n", s.ReplayDiskOK)
	fmt.Printf("artifacts written to %s/\n", *out)
	if !s.OK {
		fmt.Fprintln(os.Stderr, "VERIFICATION FAILED")
		os.Exit(1)
	}
	fmt.Println("VERIFICATION PASS")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "genartifacts:", err)
	os.Exit(1)
}

// replayFromDiskMatches 从磁盘读取回放 JSON，解码 Log 后重新 Playback，
// 并与文件中记录的逐帧哈希逐一比较。
func replayFromDiskMatches(path string, wantHex []string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var onDisk struct {
		Log            replay.Log `json:"log"`
		RecordedHashes []string   `json:"recorded_hashes"`
	}
	if err := json.Unmarshal(b, &onDisk); err != nil {
		return false, err
	}
	got, err := replay.Playback(onDisk.Log)
	if err != nil {
		return false, err
	}
	if len(got) != len(wantHex) || len(onDisk.RecordedHashes) != len(wantHex) {
		return false, nil
	}
	for i, h := range got {
		if fmt.Sprintf("%016x", h) != wantHex[i] {
			return false, nil
		}
	}
	return true, nil
}
