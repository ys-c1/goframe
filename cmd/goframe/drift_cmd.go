package main

import (
	"flag"
	"fmt"
	"os"

	"goframe/internal/drift"
)

func driftCmd(args []string) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	out := fs.String("out", "artifacts", "产物输出目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rep, err := drift.Generate(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate failed:", err)
		return 1
	}
	fmt.Println("== 三份快照：漂移起始帧 / 起始偏差 / 规范化前后哈希 ==")
	for _, c := range rep.Cases {
		driftTick := "-"
		if c.DriftStartTickInclCheckpoint != nil {
			driftTick = fmt.Sprintf("%d", *c.DriftStartTickInclCheckpoint)
		}
		fmt.Printf("%s N=%d C=%d drift_start=%s dev_start_raw=%d (%.6f 单位) dev_end_raw=%d\n",
			c.Spec.ID, c.SpecN, c.Spec.Checkpoint, driftTick,
			c.DeviationAtDriftStartRaw, c.DeviationAtDriftStartUnit, c.DeviationAtEndRaw)
		fmt.Printf("  direct@C=%s naive@C=%s normalized@C=%s norm_all_match=%v formula_residual=%d\n",
			c.CheckpointHashDirect, c.NaiveCheckpointHash, c.NormalizedCheckpointHash,
			c.NormalizedDriftStartTick == nil, c.FormulaMaxAbsResidual)
	}
	l := rep.LatentSnapshot
	fmt.Printf("latent: 整数X首次差1单位 f=%d tick=%d dx=%d raw; normalized_all_hash_equal=%v\n",
		l.XWholeUnitOnsetF, l.XWholeUnitOnsetTick, l.OnsetDeltaXRaw, l.NormalizedAllHashEqual)
	fmt.Println("artifacts written to", *out)
	return 0
}

func verifyCmd(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	out := fs.String("out", "artifacts", "产物目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rep, err := drift.VerifyArtifacts(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify failed:", err)
		return 1
	}
	for _, c := range rep.Cases {
		fmt.Printf("verify %s: checked %d frames | normalized-legacy==direct:%v canonical-v2==direct:%v raw-order-equal:%v naive-diverges@C:%v\n",
			c.ID, c.FramesChecked, c.NormalizedMatch, c.CanonicalMatch, c.RawOrderEqual, c.NaiveStillDiverges)
		if c.NormalizedMatch && c.CanonicalMatch {
			fmt.Printf("  %s: 规范化后漂移帧号 = 无（全部逐帧哈希与直跑一致）\n", c.ID)
		}
	}
	fmt.Printf("verify latent: normalized replay all hashes == direct: %v\n", rep.LatentOK)
	if !rep.OK {
		fmt.Println("VERIFY FAIL")
		return 1
	}
	fmt.Println("VERIFY OK: 规范化序快照逐帧哈希与直跑完全一致，漂移帧号不再出现")
	return 0
}
