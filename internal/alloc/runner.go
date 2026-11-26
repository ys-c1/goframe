package alloc

import (
	"fmt"
	"strings"
)

// SeqReport 是单组序列在两种引擎上的实测结果。
type SeqReport struct {
	Index             int
	TotalSpawns       int     // 累计生成实体次数
	Reuses            int     // 槽位复用次数（生成命中自由表的次数）
	HighWater         int     // 槽位历史峰值 = 总生成 - 复用
	RecycledHash      uint64  // 槽位复用引擎终态指纹
	RecycledRaw       []int32 // 槽位复用引擎哈希输入流（含空洞零值）
	FirstSurvivorSlot int     // 首个共同存活者所在槽位
	MonoHash          uint64  // 单调编号引擎终态指纹
	MonoRaw           []int32 // 单调编号引擎哈希输入流
}

// Report 是一个场景三个序列的汇总。
type Report struct {
	Scenario  string
	Sequences [3]SeqReport
}

// Run 在两种引擎上执行同一条操作序列。
func run(seqIdx int, sc Scenario, ops []Op, pushFrame int) SeqReport {
	rec := NewRecycled()
	mono := NewMonotonic()

	// 按帧分桶，保证两条序列帧数对齐、积分次数一致。
	last := pushFrame
	buckets := make([][]Op, last+1)
	for _, op := range ops {
		buckets[op.Frame] = append(buckets[op.Frame], op)
	}
	for _, op := range pushSurvivors(pushFrame, sc.Survivors) {
		buckets[pushFrame] = append(buckets[pushFrame], op)
	}

	tick := 0
	for f := 1; f <= last; f++ {
		for _, op := range buckets[f] {
			switch op.Kind {
			case OpSpawn:
				rec.spawn(op.Name)
				mono.spawn(op.Name)
			case OpDespawn:
				rec.despawn(op.Name)
				mono.despawn(op.Name)
			case OpPush:
				rec.push(op.Name, op.PushX, op.PushY)
				mono.push(op.Name, op.PushX, op.PushY)
			}
		}
		rec.integrate()
		mono.integrate()
		tick++
	}

	return SeqReport{
		Index:             seqIdx,
		TotalSpawns:       rec.spawns,
		Reuses:            rec.reuses,
		HighWater:         rec.highWater(),
		RecycledHash:      rec.hash(tick),
		RecycledRaw:       rec.Raw(),
		FirstSurvivorSlot: rec.slotOf(sc.Survivors[0]),
		MonoHash:          mono.Hash(tick),
		MonoRaw:           mono.Raw(),
	}
}

// RunAll 执行全部场景并返回报告。
func RunAll() []Report {
	scenarios := Scenarios()
	out := make([]Report, 0, len(scenarios))
	for _, sc := range scenarios {
		rep := Report{Scenario: sc.Name}
		pushFrame := sc.maxFrame() + 1
		for i, ops := range sc.Sequences {
			rep.Sequences[i] = run(i, sc, ops, pushFrame)
		}
		out = append(out, rep)
	}
	return out
}

// FormatReport 把报告渲染成可留档的纯文本日志。
// 开头固定两行汇总：复用次数行与错位量行（各场景三列，对应三组序列）。
func FormatReport(reports []Report) string {
	s := ""
	s += "# 编号复用 vs 单调编号 对照实验日志\n"
	s += "# 槽位规格: 每实体 4 个 int32 字 (posX,posY,velX,velY)\n"
	s += "# 错位量单位为哈希输入流字；理论值 = 复用次数 × 槽位字数(4)\n\n"
	// 两行汇总：上行复用次数 R，下行错位量 D；每个场景一个 a/b/c 三元组。
	rCells := make([]string, 0, len(reports))
	dCells := make([]string, 0, len(reports))
	for _, rep := range reports {
		rCells = append(rCells, fmt.Sprintf("%s[%d/%d/%d]",
			rep.Scenario, rep.Sequences[0].Reuses, rep.Sequences[1].Reuses, rep.Sequences[2].Reuses))
		dCells = append(dCells, fmt.Sprintf("%s[%d/%d/%d]",
			rep.Scenario,
			rep.Sequences[0].Reuses*slotWords, rep.Sequences[1].Reuses*slotWords, rep.Sequences[2].Reuses*slotWords))
	}
	s += "# 两行汇总（方括号内依次为 seq0/seq1/seq2）\n"
	s += "复用次数 R:         " + strings.Join(rCells, "  ") + "\n"
	s += "错位量 D=R×4(字数): " + strings.Join(dCells, "  ") + "\n\n"
	for _, rep := range reports {
		s += fmt.Sprintf("## 场景 %s\n", rep.Scenario)
		s += "序列:                 seq0      seq1      seq2\n"
		s += fmt.Sprintf("总生成次数:           %-9d %-9d %d\n",
			rep.Sequences[0].TotalSpawns, rep.Sequences[1].TotalSpawns, rep.Sequences[2].TotalSpawns)
		s += fmt.Sprintf("复用次数 R:           %-9d %-9d %d\n",
			rep.Sequences[0].Reuses, rep.Sequences[1].Reuses, rep.Sequences[2].Reuses)
		s += fmt.Sprintf("槽位高水位(生成-R):   %-9d %-9d %d\n",
			rep.Sequences[0].HighWater, rep.Sequences[1].HighWater, rep.Sequences[2].HighWater)
		// 以零复用序列为基准：其哈希流长度是无错位基线。
		base := rep.Sequences[2]
		shifts := [3]int{}
		for i := range rep.Sequences {
			shifts[i] = (len(base.RecycledRaw) - len(rep.Sequences[i].RecycledRaw))
		}
		s += fmt.Sprintf("错位量 D(实测字数):   %-9d %-9d %d\n", shifts[0], shifts[1], shifts[2])
		s += fmt.Sprintf("错位量 R×4(理论):     %-9d %-9d %d\n",
			rep.Sequences[0].Reuses*slotWords, rep.Sequences[1].Reuses*slotWords, rep.Sequences[2].Reuses*slotWords)
		s += fmt.Sprintf("首存活者槽位:         %-9d %-9d %d\n",
			rep.Sequences[0].FirstSurvivorSlot, rep.Sequences[1].FirstSurvivorSlot, rep.Sequences[2].FirstSurvivorSlot)
		s += fmt.Sprintf("复用引擎哈希:         %016x %016x %016x\n",
			rep.Sequences[0].RecycledHash, rep.Sequences[1].RecycledHash, rep.Sequences[2].RecycledHash)
		s += fmt.Sprintf("单调引擎哈希:         %016x %016x %016x\n",
			rep.Sequences[0].MonoHash, rep.Sequences[1].MonoHash, rep.Sequences[2].MonoHash)
		s += "\n"
	}
	return s
}
