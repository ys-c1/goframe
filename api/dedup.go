package api

// 本文件处理同一 tick 内的重复指令。
//
// 结论先写在前面：把「倍数为 m 的相同指令」压成一条时，只能把 m 折进冲量
// （归并），不能只保留一条（丢弃）。定点加法可按 2^32 回绕，m 次 d 与一次
// m*d 在模 2^32 下逐位相同，因此归并是精确的；丢弃则会少施加 (m-1)*d。
// 推导与审计见 docs/dedup.md。

// dedupKey 是完全相同的判定键，按执行时实际生效的 int32 冲量归一：
// 帧号、实体、两个冲量分量都相等才算同一条指令。
type dedupKey struct {
	tick int
	body int
	dx   int32
	dy   int32
}

func keyOf(c Command) dedupKey {
	return dedupKey{tick: c.Tick, body: c.Body, dx: int32(c.PushX), dy: int32(c.PushY)}
}

// sameCommand 是 Dedup 的相等判定，与 keyOf 一致，均按实际生效的 int32 比较。
func sameCommand(a, b Command) bool {
	return a.Tick == b.Tick && a.Body == b.Body && int32(a.PushX) == int32(b.PushX) && int32(a.PushY) == int32(b.PushY)
}

// DedupCommands 把同一 tick 内完全相同的指令折叠为一条，并把出现次数折进冲量。
//
// 结果保持每组首次出现的顺序；不修改入参。折叠是精确的：对每个实体与每个
// 速度分量，折叠前后本 tick 的冲量总和相同（定点加法模 2^32）。
//
// 无重复时不复制、原样返回入参，因此常态帧只付一次判定的代价。
//
// 开销：判定 HasDuplicates 为 O(k^2) 次四字段比较；有重复时才 O(k) 复制并
// O(k*g) 原地归并（g 为不同组数）。k<32 时判定为整帧耗时的 0.1~2 倍，
// 每帧指令数上百时判定开始比执行还贵，此时应按 body 分桶或改用位图。
// 见 docs/dedup.md 的开销一节。
func DedupCommands(cmds []Command) []Command {
	if !HasDuplicates(cmds) {
		return cmds
	}
	out := make([]Command, len(cmds))
	copy(out, cmds)
	return dedupInPlaceLinear(out)
}

// HasDuplicates 报告是否存在完全相同的两条指令（按实际生效的 int32 冲量）。
// 最坏 O(k^2) 次比较，提前命中即返回；常态帧（无重复）会走满。
func HasDuplicates(cmds []Command) bool {
	n := len(cmds)
	for i := 1; i < n; i++ {
		for j := 0; j < i; j++ {
			if sameCommand(cmds[i], cmds[j]) {
				return true
			}
		}
	}
	return false
}

// DedupInPlace 在原切片上归并并返回其前缀；只允许调用方丢弃原切片时使用。
// 返回值与 DedupCommands 逐项相同。
func DedupInPlace(cmds []Command) []Command {
	if len(cmds) == 0 {
		return nil
	}
	counts := make([]int, 0, len(cmds))
	index := make(map[dedupKey]int, len(cmds))
	n := 0
	for _, c := range cmds {
		k := keyOf(c)
		if i, ok := index[k]; ok {
			counts[i]++
			continue
		}
		index[k] = n
		cmds[n] = c
		n++
		counts = append(counts, 1)
	}
	cmds = cmds[:n]
	for i := range cmds {
		if counts[i] == 1 {
			continue
		}
		cmds[i].PushX = foldImpulse(cmds[i].PushX, counts[i])
		cmds[i].PushY = foldImpulse(cmds[i].PushY, counts[i])
	}
	return cmds
}

// dedupInPlaceLinear 原地线性归并，写回前缀并返回。
func dedupInPlaceLinear(cmds []Command) []Command {
	counts := make([]int, len(cmds))
	n := 0
	for _, c := range cmds {
		hit := -1
		for i := 0; i < n; i++ {
			if sameCommand(cmds[i], c) {
				hit = i
				break
			}
		}
		if hit >= 0 {
			counts[hit]++
			continue
		}
		cmds[n] = c
		counts[n] = 1
		n++
	}
	cmds = cmds[:n]
	for i := range cmds {
		if counts[i] != 1 {
			cmds[i].PushX = foldImpulse(cmds[i].PushX, counts[i])
			cmds[i].PushY = foldImpulse(cmds[i].PushY, counts[i])
		}
	}
	return cmds
}

// foldImpulse 返回 n 次相同定点冲量叠加后的值。int32 乘法与 int64 转回 int32
// 都是模 2^32 回绕，与执行 n 次定点加法逐位一致。
func foldImpulse(d, n int) int {
	return int(int32(int64(d) * int64(n)))
}

// dropDuplicates 是「只保留每组首条、丢弃其余」的旧基线，仅用于测试与审计，
// 不对外暴露。它对非零冲量必然丢量，见 docs/dedup.md。
func dropDuplicates(cmds []Command) []Command {
	if len(cmds) == 0 {
		return nil
	}
	seen := make(map[dedupKey]struct{}, len(cmds))
	out := make([]Command, 0, len(cmds))
	for _, c := range cmds {
		k := keyOf(c)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, c)
	}
	return out
}

// ImpulseLoss 是一组相同指令若按「只留一条」丢弃时，相对原序执行少施加的冲量。
type ImpulseLoss struct {
	Tick  int
	Body  int
	DX    int // 每条被丢指令的冲量分量（原始 Q16.16）
	DY    int
	Count int // 该组总条数
	// LostX/LostY 为 (Count-1)*DX/DY，按 int32 回绕，与状态差分逐位一致。
	LostX int
	LostY int
}

// DropLoss 按 (Tick, Body, 冲量) 分组，报告丢弃副本会丢的冲量；顺序为各组首次出现顺序。
func DropLoss(cmds []Command) []ImpulseLoss {
	if len(cmds) == 0 {
		return nil
	}
	out := make([]ImpulseLoss, 0)
	index := make(map[dedupKey]int, len(cmds))
	for _, c := range cmds {
		k := keyOf(c)
		if i, ok := index[k]; ok {
			out[i].Count++
			out[i].LostX = foldImpulse(c.PushX, out[i].Count-1)
			out[i].LostY = foldImpulse(c.PushY, out[i].Count-1)
			continue
		}
		index[k] = len(out)
		out = append(out, ImpulseLoss{Tick: c.Tick, Body: c.Body, DX: int(int32(c.PushX)), DY: int(int32(c.PushY)), Count: 1})
	}
	trimmed := out[:0]
	for _, l := range out {
		if l.Count > 1 {
			trimmed = append(trimmed, l)
		}
	}
	return trimmed
}

// DropIsLossless 报告按「只留一条」处理这批指令是否完全不改变状态贡献。
// 仅当所有重复组的冲量都为零时成立。
func DropIsLossless(cmds []Command) bool {
	for _, l := range DropLoss(cmds) {
		if l.LostX != 0 || l.LostY != 0 {
			return false
		}
	}
	return true
}

// Independent 报告两条指令的目标与字段交集是否为空。
// 目标不同则不相交；目标相同则比较非零写入的字段（PushX 对应 velX，PushY 对应 velY）。
// 零分量不写状态，因此不参与交集。
func Independent(a, b Command) bool {
	if a.Body != b.Body {
		return true
	}
	if a.PushX != 0 && b.PushX != 0 {
		return false
	}
	if a.PushY != 0 && b.PushY != 0 {
		return false
	}
	return true
}
