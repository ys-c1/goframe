package parallel

import (
	"fmt"
	"strings"
)

const dash = "----------------------------------------------------------------------"

// RenderLog 生成完整文本日志：串行基准、三栏对拍表、差异上界验证、
// 按字段的共享写入统计与统计口径说明。
func (r *ExperimentReport) RenderLog() string {
	var b strings.Builder
	fmt.Fprintf(&b, "并发步进等价性对拍实验日志\n")
	fmt.Fprintf(&b, "%s\n", dash)
	fmt.Fprintf(&b, "场景: 实体数 N=%d, 帧数 T=%d, 子步数=%d, 重力=%d, 耦合右移=%d\n",
		r.Cfg.Bodies, r.Cfg.Frames, r.Cfg.SubSteps, r.Cfg.Gravity, r.Cfg.CouplingShift)
	fmt.Fprintf(&b, "串行基准末态哈希(朴素计划)   = %016x\n", r.SerialNaiveHash)
	fmt.Fprintf(&b, "串行基准末态哈希(两阶段计划) = %016x  (两者相同=%v)\n",
		r.SerialStagedHash, r.SerialNaiveHash == r.SerialStagedHash)
	fmt.Fprintf(&b, "单操作增量上界 M=%d（Q16.16 原始单位），单位置最大写入次数 L=%d\n", r.MaxDelta, r.MaxDepth)
	fmt.Fprintf(&b, "%s\n\n", dash)

	// 表一：三栏 —— 并发度 | 末态哈希 | 差异与条件判定
	b.WriteString("表一  三档并发度对拍（三栏：并发度 / 末态哈希 / 差异与条件判定）\n")
	head := fmt.Sprintf("%-6s | %-18s | %s\n", "并发度", "末态哈希", "差异 D / 不同位置数 / 条件判定 / 共享写入 S")
	b.WriteString(head)
	b.WriteString(strings.Repeat("-", len(head)) + "\n")
	for _, lr := range r.Levels {
		naiveVerdict := "互不相交成立"
		if !lr.NaiveDisjoint {
			naiveVerdict = "存在共享写入(条件不成立)"
		}
		fmt.Fprintf(&b, "W=%-3d | 朴素交错 %016x | D=%d diffLocs=%d %s S=%d\n",
			lr.Workers, lr.NaiveHash, lr.NaiveDistance, lr.NaiveDiffLocs, naiveVerdict, lr.NaiveSharedWrites)
		stagedVerdict := "互不相交成立"
		if !lr.StagedDisjoint {
			stagedVerdict = "存在共享写入(条件不成立)"
		}
		fmt.Fprintf(&b, "%-6s | 修复并发 %016x | D=%d diffLocs=%d %s S=%d\n",
			"", lr.StagedHash, lr.StagedDistance, lr.StagedDiffLocs, stagedVerdict, lr.StagedSharedWrites)
		match := "等于串行"
		if lr.StagedHash != r.SerialNaiveHash {
			match = "不等于串行(失败)"
		}
		fmt.Fprintf(&b, "%-6s | 串行基准 %016x | %s\n", "", r.SerialNaiveHash, match)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%s\n\n", dash)

	// 表二：差异上界 D <= 2*M*S*L
	b.WriteString("表二  差异上界验证（朴素计划，确定性交错执行）\n")
	fmt.Fprintf(&b, "%-6s | %8s | %8s | %10s | %12s | %s\n",
		"并发度", "S(共享)", "M", "L", "2*M*S*L", "实测 D / 判定")
	for _, lr := range r.Levels {
		bound, ok := r.BoundOK(&lr)
		verdict := "D<=上界 成立"
		if !ok {
			verdict = "违反上界(失败)"
		}
		fmt.Fprintf(&b, "W=%-3d | %8d | %8d | %10d | %12d | D=%d %s\n",
			lr.Workers, lr.NaiveSharedWrites, r.MaxDelta, r.MaxDepth, bound, lr.NaiveDistance, verdict)
	}
	fmt.Fprintf(&b, "\n上界形式 D <= 2·M·S·L：D 与共享写入次数 S 成正比；M、L 是只取决于场景规模的常数。\n")
	fmt.Fprintf(&b, "%s\n\n", dash)

	// 表三：共享写入按字段统计，并与实体数一起报
	b.WriteString("表三  共享写入按字段统计（朴素计划；统计单位为字段位置 (实体,字段)）\n")
	fmt.Fprintf(&b, "%-6s | %-6s | %-10s | %-12s | %-12s | %s\n",
		"并发度", "字段", "总写入", "共享写入 S", "涉及实体数", "实体总数 N")
	for _, lr := range r.Levels {
		for f := 0; f < numPhysical; f++ {
			st := lr.NaivePerField[f]
			fmt.Fprintf(&b, "W=%-3d | %-6s | %10d | %12d | %12d | %d\n",
				lr.Workers, st.Field.String(), st.Writes, st.SharedWrites, st.SharedBodies, r.Cfg.Bodies)
		}
		fmt.Fprintf(&b, "%-6s | %-6s | %10s | S 合计=%-5d | 共享实体=%-5d | %d\n",
			"", "合计", "", lr.NaiveSharedWrites, lr.NaiveSharedBodies, r.Cfg.Bodies)
		fmt.Fprintf(&b, "%-6s | 发件箱共享写入=%d（修复版必须为 0）；总写入次数=%d\n\n",
			"", lr.NaiveOutboxShared, lr.NaiveTotalWrites)
	}
	fmt.Fprintf(&b, "%s\n", dash)
	b.WriteString(renderMethodology())
	return b.String()
}

func renderMethodology() string {
	return `统计口径说明
1. 统计粒度：按字段，不按实体。最小单位是字段位置 Loc=(实体编号, 字段)，
   字段取 velX/velY/posX/posY 四个物理字段；同一实体的四个字段分别计数。
2. worker 归属：每个操作按其写入实体的属主 Owner(Body)=floor(Body*W/N) 归属，
   实体按编号连续切块分区，一个实体的全部字段归同一 worker。
3. 共享写入判定：位置 L 被两个及以上 worker 写入即构成共享写入；
   其共享写入次数 S(L)=非属主 worker 对 L 的写入次数。
4. 总共享写入次数 S=对所有字段位置求和 Σ S(L)，按字段分别汇总；
   同时报告涉及共享写入的不同实体数（去重后的实体数）。
5. 共享写入的来源：环上耦合 i -> (i+1) mod N。仅当目标实体与源实体
   分属不同 worker（分区边界处的环边）时，源 worker 对目标速度字段的
   写入才计入 S；分区内部的耦合写入由属主完成，不计入。
6. 修复版（两阶段计划）把耦合冲量重定向到源实体私有的发件箱字段：
   源 worker 只写自己的发件箱，目标实体的属主在阶段屏障后跨 worker 读取
   （读不违反写入集合互不相交条件），故所有并发度下 S=0、条件成立。
`
}
