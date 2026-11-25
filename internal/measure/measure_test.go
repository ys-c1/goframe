package measure

import (
	"bytes"
	"strings"
	"testing"

	"goframe/internal/hash"
)

func TestRunIsBitIdentical(t *testing.T) {
	for _, bodies := range []int{1, 3, 16, 100} {
		tier, err := Run(Config{Label: "sparse", Bodies: bodies, ActiveBodies: 1, Frames: 50, SubStep: 4, Gravity: 3, Rounds: 3})
		if err != nil {
			t.Fatal(err)
		}
		if !tier.Equivalence.Equal() {
			t.Fatalf("bodies=%d 逐帧对拍不等价: %+v", bodies, tier.Equivalence)
		}
		if tier.Compare.DiffBits != 0 || tier.Compare.DiffFrames != 0 {
			t.Fatalf("bodies=%d 序列有差异: %+v", bodies, tier.Compare)
		}
		if tier.Compare.FirstDiffAt != -1 {
			t.Fatalf("bodies=%d 首个差异帧应为 -1: %d", bodies, tier.Compare.FirstDiffAt)
		}
		if len(tier.FullHashes) != 50 || len(tier.IncHashes) != 50 {
			t.Fatalf("序列长度不对: %d/%d", len(tier.FullHashes), len(tier.IncHashes))
		}
	}
}

func TestSequencesArchivedSeparately(t *testing.T) {
	tier, err := Run(Config{Label: "sparse", Bodies: 4, ActiveBodies: 1, Frames: 6, SubStep: 1, Gravity: 0, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	var full, inc, cmp bytes.Buffer
	if err := WriteSequence(&full, tier, "full"); err != nil {
		t.Fatal(err)
	}
	if err := WriteSequence(&inc, tier, "inc"); err != nil {
		t.Fatal(err)
	}
	if err := WriteCompare(&cmp, tier); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(full.String(), "inc=") || strings.Contains(inc.String(), "full=") {
		t.Fatal("两版序列应各自独立存档，不混放")
	}
	lineCount := func(b *bytes.Buffer) int {
		n := 0
		for _, l := range strings.Split(strings.TrimSpace(b.String()), "\n") {
			if !strings.HasPrefix(l, "#") {
				n++
			}
		}
		return n
	}
	if lineCount(&full) != 6 || lineCount(&inc) != 6 || lineCount(&cmp) != 6 {
		t.Fatalf("行数不对: full=%d inc=%d cmp=%d",
			lineCount(&full), lineCount(&inc), lineCount(&cmp))
	}
	for _, l := range strings.Split(strings.TrimSpace(cmp.String()), "\n") {
		if strings.HasPrefix(l, "#") {
			continue
		}
		if !strings.HasSuffix(l, "xor=0000000000000000") {
			t.Fatalf("差值不为零: %s", l)
		}
	}
}

func TestCompareSequencesDetectsSingleBit(t *testing.T) {
	a := []uint64{0, 1, 0xff, 0xff}
	b := []uint64{0, 1, 0xfe, 0xff}
	r := hash.CompareSequences(a, b)
	if r.DiffBits != 1 || r.DiffFrames != 1 || r.FirstDiffAt != 2 {
		t.Fatalf("单位差异未按预期统计: %+v", r)
	}
}

func TestCompareSequencesLengthMismatch(t *testing.T) {
	r := hash.CompareSequences([]uint64{1, 2, 3}, []uint64{1, 2})
	if r.Frames != 2 || r.FirstDiffAt != 2 {
		t.Fatalf("长度不同应报告差异: %+v", r)
	}
}

func TestWriteTableAndBitCheck(t *testing.T) {
	tiers := []Tier{
		{Config: Config{Label: "sparse", Bodies: 8}, Compare: hash.BitResult{Frames: 10, FirstDiffAt: -1}},
	}
	var table, bits bytes.Buffer
	if err := WriteSummary(&table, tiers); err != nil {
		t.Fatal(err)
	}
	if err := WriteBitCheck(&bits, tiers); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), "逐位相同") {
		t.Fatalf("表里应给出等价判定: %s", table.String())
	}
	if !strings.Contains(bits.String(), "不同位总数") {
		t.Fatalf("逐位表缺少列: %s", bits.String())
	}
}
