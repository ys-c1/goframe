package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goframe/api"
	"goframe/internal/pack"
	"goframe/replay"
)

// 场景冲量（Q16.16 raw 单位），与 16 帧指令调度一致。
const (
	impA = 99840
	impB = 13312
	impC = 50368
	impD = 81930
)

// 漂移上限（raw 单位）。
const (
	posLimitRaw int64 = 65536 // 实值 1.000000
	velLimitRaw int64 = 2048  // 实值 0.031250
)

// 三档实验位宽。
var tierBits = []int{21, 24, 28}

type snapLine struct {
	Kind string  `json:"kind"`
	Bits *int    `json:"bits,omitempty"`
	Tick int     `json:"tick"`
	Raw  []int32 `json:"raw"`
}

type tierStat struct {
	bits          int
	posMaxErr     int64
	velMaxErr     int64
	firstPosTick  int
	firstVelTick  int
	packedBytes   int
	restoredBytes int
}

// scenarioFrames 构造 16 帧确定性指令序列。
func scenarioFrames() []replay.Frame {
	frames := make([]replay.Frame, 16)
	add := func(tick, body, push int) {
		frames[tick].Commands = append(frames[tick].Commands,
			api.Command{Tick: tick, Body: body, PushX: push})
	}
	// A 脉冲：tick 0..7 依次打 body 0..7，tick 8..15 滚动打 body 0..7。
	for t := 0; t < 16; t++ {
		add(t, t%8, impA)
	}
	// B 脉冲：第 8 帧起，body (t-7)%8。
	for t := 8; t < 16; t++ {
		add(t, (t-7)%8, impB)
	}
	// C 脉冲：第 12 帧起，body (t-6)%8。
	for t := 12; t < 16; t++ {
		add(t, (t-6)%8, impC)
	}
	// D 脉冲：第 14 帧起，body (t-13)%8。
	for t := 14; t < 16; t++ {
		add(t, (t-13)%8, impD)
	}
	return frames
}

func writeJSONL(path string, lines []snapLine) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, ln := range lines {
		if err := enc.Encode(ln); err != nil {
			return err
		}
	}
	return w.Flush()
}

func abs32(x int32) int64 {
	v := int64(x)
	if v < 0 {
		return -v
	}
	return v
}

// snap 生成全量与三档位宽的两套快照并打印逐档对照。
func snap(args []string, outDir string) int {
	opt := api.Options{Bodies: 8, SubSteps: 1, Gravity: 0}
	frames := scenarioFrames()
	world, err := api.New(opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "new world:", err)
		return 1
	}
	var full [][]int32
	for _, fr := range frames {
		if err := world.Step(fr.Commands); err != nil {
			fmt.Fprintln(os.Stderr, "step:", err)
			return 1
		}
		snap := world.Snapshot()
		cp := make([]int32, len(snap))
		copy(cp, snap)
		full = append(full, cp)
	}
	bodies := opt.Bodies

	fullLines := make([]snapLine, len(full))
	for t, raw := range full {
		fullLines[t] = snapLine{Kind: "full", Tick: t, Raw: raw}
	}
	if err := writeJSONL(filepath.Join(outDir, "full.jsonl"), fullLines); err != nil {
		fmt.Fprintln(os.Stderr, "write full:", err)
		return 1
	}

	stats := make([]tierStat, 0, len(tierBits))
	for _, bits := range tierBits {
		data, err := pack.Pack(full, bits)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pack:", err)
			return 1
		}
		restored, rb, err := pack.Unpack(data)
		if err != nil || rb != bits {
			fmt.Fprintln(os.Stderr, "unpack:", err)
			return 1
		}
		b := bits
		lines := make([]snapLine, len(restored))
		st := tierStat{bits: bits, firstPosTick: -1, firstVelTick: -1, packedBytes: len(data)}
		for t, raw := range restored {
			lines[t] = snapLine{Kind: "restored", Bits: &b, Tick: t, Raw: raw}
			for i := 0; i < bodies; i++ {
				pe := abs32(full[t][4*i] - raw[4*i])
				if pe2 := abs32(full[t][4*i+1] - raw[4*i+1]); pe2 > pe {
					pe = pe2
				}
				if pe > 0 && st.firstPosTick < 0 {
					st.firstPosTick = t
				}
				if pe > st.posMaxErr {
					st.posMaxErr = pe
				}
				ve := abs32(full[t][4*i+2] - raw[4*i+2])
				if ve2 := abs32(full[t][4*i+3] - raw[4*i+3]); ve2 > ve {
					ve = ve2
				}
				if ve > 0 && st.firstVelTick < 0 {
					st.firstVelTick = t
				}
				if ve > st.velMaxErr {
					st.velMaxErr = ve
				}
			}
		}
		rPath := filepath.Join(outDir, fmt.Sprintf("restored_b%d.jsonl", bits))
		pPath := filepath.Join(outDir, fmt.Sprintf("packed_b%d.bin", bits))
		if err := writeJSONL(rPath, lines); err != nil {
			fmt.Fprintln(os.Stderr, "write restored:", err)
			return 1
		}
		if err := os.WriteFile(pPath, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write packed:", err)
			return 1
		}
		st.restoredBytes = 0
		for _, ln := range lines {
			buf, _ := json.Marshal(ln)
			st.restoredBytes += len(buf) + 1
		}
		stats = append(stats, st)
	}

	fullBytes := 0
	for _, ln := range fullLines {
		buf, _ := json.Marshal(ln)
		fullBytes += len(buf) + 1
	}
	reportPath := filepath.Join(outDir, "report.txt")
	report := renderReport(stats, fullBytes)
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write report:", err)
		return 1
	}
	fmt.Print(report)
	return 0
}

func realUnit(raw int64) float64 { return float64(raw) / 65536.0 }

func renderReport(stats []tierStat, fullBytes int) string {
	var b strings.Builder
	fmt.Fprintln(&b, "goframe 按位打包快照实验")
	fmt.Fprintln(&b, "场景: frames=16 bodies=8 substeps=1 gravity=0，冲量 A=99840 B=13312 C=50368 D=81930（第8帧起加 B，第12帧起加 C，第14帧起加 D）")
	fmt.Fprintln(&b, "通道顺序: 每实体 [posX posY velX velY]，Q16.16 raw，实值 = raw/65536")
	fmt.Fprintf(&b, "漂移上限: 位置 %d raw (%.6f 实值) | 速度 %d raw (%.6f 实值)\n",
		posLimitRaw, realUnit(posLimitRaw), velLimitRaw, realUnit(velLimitRaw))
	fmt.Fprintf(&b, "位宽反解: MinBits(位置上限)=%d, MinBits(速度上限)=%d, 绑定最小位宽=%d\n",
		pack.MinBits(posLimitRaw), pack.MinBits(velLimitRaw),
		pack.MinBits(velLimitRaw))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "位宽   | 位置最大误差(raw/实值)             | 位置上限(raw/实值)             | 速度最大误差(raw/实值)             | 速度上限(raw/实值)             | 首次偏移帧(位置/速度) | 判定   | 紧凑/全量字节")
	fmt.Fprintln(&b, "------------------------------------------------------------------------------------------------------------------------------------------------------------------------------")
	for _, st := range stats {
		verdict := "PASS"
		if st.posMaxErr > posLimitRaw || st.velMaxErr > velLimitRaw {
			verdict = "FAIL"
		}
		fmt.Fprintf(&b, "%-4d | %6d / %.6f            | %6d / %.6f         | %6d / %.6f            | %6d / %.6f         | %4d / %-4d          | %s | %d/%d\n",
			st.bits,
			st.posMaxErr, realUnit(st.posMaxErr),
			posLimitRaw, realUnit(posLimitRaw),
			st.velMaxErr, realUnit(st.velMaxErr),
			velLimitRaw, realUnit(velLimitRaw),
			st.firstPosTick, st.firstVelTick,
			verdict, st.packedBytes, fullBytes)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "快照文件（每档位宽两版：紧凑比特流 + 逐行同构还原版，另附 32 位无损基准）:")
	fmt.Fprintln(&b, "  full.jsonl          全量(32位无损)快照，基准版")
	for _, st := range stats {
		fmt.Fprintf(&b, "  restored_b%d.jsonl   %d 位打包还原版，与 full.jsonl 逐行同构可直接对拍\n", st.bits, st.bits)
		fmt.Fprintf(&b, "  packed_b%d.bin       %d 位紧凑比特流(魔数 GFSNAP01 + 位宽头 + MSB-first 码流)\n", st.bits, st.bits)
	}
	return b.String()
}
