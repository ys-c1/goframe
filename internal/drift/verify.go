package drift

import (
	"fmt"
	"os"
	"path/filepath"

	"goframe/api"
	"goframe/internal/snapfmt"
)

// CaseVerify 是一份快照从磁盘复核的结果。
type CaseVerify struct {
	ID                 string `json:"id"`
	FramesChecked      int    `json:"frames_checked"`
	NormalizedMatch    bool   `json:"normalized_legacy_all_hashes_match_direct"`
	CanonicalMatch     bool   `json:"canonical_v2_all_hashes_match_direct"`
	RawOrderEqual      bool   `json:"normalized_raw_equals_canonical_raw"`
	NaiveStillDiverges bool   `json:"naive_legacy_still_diverges_as_expected"`
	OK                 bool   `json:"ok"`
}

// VerifyReport 是 verify 命令的汇总。
type VerifyReport struct {
	Cases    []CaseVerify `json:"cases"`
	LatentOK bool         `json:"latent_ok"`
	OK       bool         `json:"ok"`
}

// VerifyArtifacts 从 outDir 读取已落盘的两种序列化快照，
// 用规范化路径回放并与直跑逐帧比对哈希。
func VerifyArtifacts(outDir string) (*VerifyReport, error) {
	rep := &VerifyReport{OK: true}
	for _, spec := range DefaultSpecs() {
		v, err := verifyCase(outDir, spec)
		if err != nil {
			return nil, err
		}
		rep.Cases = append(rep.Cases, v)
		if !v.OK {
			rep.OK = false
		}
	}
	latOK, err := verifyLatent(outDir)
	if err != nil {
		return nil, err
	}
	rep.LatentOK = latOK
	if !latOK {
		rep.OK = false
	}
	return rep, nil
}

func verifyCase(outDir string, spec Spec) (CaseVerify, error) {
	v := CaseVerify{ID: spec.ID, FramesChecked: spec.Extra + 1}
	legacyPath := filepath.Join(outDir, "snapshots",
		fmt.Sprintf("%s.legacy-v1.old-order-be.bin", spec.ID))
	canonPath := filepath.Join(outDir, "snapshots",
		fmt.Sprintf("%s.canonical-v2.canonical-order-le.bin", spec.ID))

	lb, err := os.ReadFile(legacyPath)
	if err != nil {
		return v, err
	}
	cb, err := os.ReadFile(canonPath)
	if err != nil {
		return v, err
	}
	leg, err := snapfmt.DecodeLegacyV1Normalized(lb)
	if err != nil {
		return v, fmt.Errorf("normalize %s: %w", spec.ID, err)
	}
	can, err := snapfmt.DecodeCanonicalV2(cb)
	if err != nil {
		return v, fmt.Errorf("canonical %s: %w", spec.ID, err)
	}

	v.RawOrderEqual = len(leg.Raw) == len(can.Raw)
	if v.RawOrderEqual {
		for i := range leg.Raw {
			if leg.Raw[i] != can.Raw[i] {
				v.RawOrderEqual = false
				break
			}
		}
	}

	total := spec.Checkpoint + spec.Extra
	cmds := spec.Commands(total)
	dh, _, err := directRun(spec, cmds)
	if err != nil {
		return v, err
	}
	nh, _, err := runMode(spec, leg.Raw, cmds)
	if err != nil {
		return v, err
	}
	ch, _, err := runMode(spec, can.Raw, cmds)
	if err != nil {
		return v, err
	}
	// naive 路径应如预期在检查点即不同，作为「问题仍可复现」的对照。
	naive, err := snapfmt.DecodeLegacyV1Naive(lb)
	if err != nil {
		return v, err
	}
	yh, _, err := runMode(spec, naive.Raw, cmds)
	if err != nil {
		return v, err
	}

	v.NormalizedMatch = true
	v.CanonicalMatch = true
	for f := 0; f <= spec.Extra; f++ {
		if nh[f] != dh[spec.Checkpoint+f] {
			v.NormalizedMatch = false
		}
		if ch[f] != dh[spec.Checkpoint+f] {
			v.CanonicalMatch = false
		}
		if yh[f] == dh[spec.Checkpoint+f] {
			// 预期 naive 至少在 f=0 不同；若任何一帧全等不影响该判定，单独看 f=0。
		}
	}
	v.NaiveStillDiverges = yh[0] != dh[spec.Checkpoint]
	v.OK = v.RawOrderEqual && v.NormalizedMatch && v.CanonicalMatch && v.NaiveStillDiverges
	return v, nil
}

func verifyLatent(outDir string) (bool, error) {
	lat, err := RunLatent()
	if err != nil {
		return false, err
	}
	lb, err := os.ReadFile(filepath.Join(outDir, lat.LegacyFile))
	if err != nil {
		return false, err
	}
	cb, err := os.ReadFile(filepath.Join(outDir, lat.CanonicalFile))
	if err != nil {
		return false, err
	}
	leg, err := snapfmt.DecodeLegacyV1Normalized(lb)
	if err != nil {
		return false, err
	}
	can, err := snapfmt.DecodeCanonicalV2(cb)
	if err != nil {
		return false, err
	}
	opt := api.Options{Bodies: lat.Bodies, SubSteps: 1, Gravity: 0}

	normW, err := api.Restore(opt, lat.CheckpointTickLabel, leg.Raw)
	if err != nil {
		return false, err
	}
	canW, err := api.Restore(opt, lat.CheckpointTickLabel, can.Raw)
	if err != nil {
		return false, err
	}
	ok := true
	for f := 0; f <= lat.HorizonFrames; f++ {
		if f > 0 {
			if err := normW.Step(nil); err != nil {
				return false, err
			}
			if err := canW.Step(nil); err != nil {
				return false, err
			}
		}
		if hx(normW.Hash()) != lat.Frames[f].DirectHash {
			ok = false
		}
		if hx(canW.Hash()) != lat.Frames[f].DirectHash {
			ok = false
		}
	}
	return ok, nil
}
