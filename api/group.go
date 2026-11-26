package api

import "sort"

// GroupCommands 按目标实体对一帧指令做稳定分组：
// 组间按目标编号升序排列，组内严格保持输入中的原始相对顺序。
// 返回分组结果与展平后的规范序列。
//
// 该规范化保证顺序无关的充要条件：
//  1. 分组键（目标编号）只取决于指令多重集本身，与输入顺序无关；
//  2. 不同组的指令两两可交换（此时组的任意交错都等价）；
//  3. 组内顺序是输入顺序的确定函数（此处取原始相对顺序）。
//
// 本内核中 Push 是速度分量上的纯平移（定点加法），任意两条指令
// 无条件可交换，故条件 2 恒成立，规范化不改变任何末态与哈希。
func GroupCommands(cmds []Command) (groups [][]Command, canonical []Command) {
	byBody := make(map[int][]Command)
	var bodies []int
	for _, c := range cmds {
		if _, ok := byBody[c.Body]; !ok {
			bodies = append(bodies, c.Body)
		}
		byBody[c.Body] = append(byBody[c.Body], c)
	}
	sort.Ints(bodies)
	for _, b := range bodies {
		g := byBody[b]
		groups = append(groups, g)
		canonical = append(canonical, g...)
	}
	return groups, canonical
}
