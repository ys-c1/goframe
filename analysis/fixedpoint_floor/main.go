// 定点截断地板演示：Q16.16 下帧长 h 被量化为离散整数值，
// 帧率提高到阈值后，单步位移的定点表示不再变化，
// 证明“提高帧率让误差趋于零”在定点实现里存在不可逾越的地板。
package main

import (
	"fmt"

	"goframe/internal/fixed"
)

func main() {
	// 理想帧长 h（秒）。位置更新 x <- x + v*h，取 v=1（One），
	// 则单步位移的理想定点原始值为 h * 2^16。
	for _, h := range []float64{1.0 / 30, 1.0 / 60, 1.0 / 120, 1.0 / 240, 1e-6} {
		ideal := h * float64(fixed.One)
		raw := int(ideal) // Q16.16 向零取整后的帧长
		step := fixed.One.Mul(fixed.FromRaw(raw))
		quantErr := 0.0
		if ideal > 0 {
			quantErr = 100 * (ideal - float64(raw)) / ideal
		}
		fmt.Printf("h=%.7fs 理想原始值=%10.2f 量化后=%6d 单步位移原始值=%6d 帧长量化损失=%6.2f%%\n",
			h, ideal, raw, step.Raw(), quantErr)
	}
	fmt.Println()
	fmt.Println("结论：h 进入 Q16.16 后被向零取整；帧率提高到某一阈值后，")
	fmt.Println("单步位移的定点表示不再变化，截断误差地板与帧率无关，只能靠加宽定点或改积分器缓解。")
}
