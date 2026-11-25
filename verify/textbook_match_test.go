package verify

import (
	"testing"

	"goframe/internal/fixed"
)

type tbBody struct{ px, py, vx, vy fixed.Value }

// TestMidpoint_MatchesTextbookQ16Reference_OnAlignedLog 把生产宽域内核与“最自然参考实现”
// （每子步立即用 fixed.Mul 量化回 Q16 的教科书中点）逐原始分量比较。
// 规范流是低位对齐的，因此两者必须逐位相同 —— 哈希值从而也与参考一致。
func TestMidpoint_MatchesTextbookQ16Reference_OnAlignedLog(t *testing.T) {
	frames := CanonicalFrames(20, 8)
	const G = 2
	for _, sub := range []int{1, 2, 4, 8} {
		grav := fixed.FromInt(G)
		h := fixed.One.Div(fixed.FromInt(sub))
		halfH := fixed.Value(h.Raw() >> 1)

		rr, err := Run(8, sub, G, frames)
		if err != nil {
			t.Fatal(err)
		}
		bodies := make([]tbBody, 8)
		for f, fr := range frames {
			for _, c := range fr.Commands {
				bodies[c.Body].vx = bodies[c.Body].vx.Add(fixed.FromRaw(c.PushX))
				bodies[c.Body].vy = bodies[c.Body].vy.Add(fixed.FromRaw(c.PushY))
			}
			for k := 0; k < sub; k++ {
				for i := range bodies {
					b := &bodies[i]
					vmidY := b.vy.Add(grav.Mul(halfH))
					b.px = b.px.Add(b.vx.Mul(h))
					b.py = b.py.Add(vmidY.Mul(h))
					b.vy = b.vy.Add(grav.Mul(h))
				}
			}
			for i := range bodies {
				got := rr.Raw[f][4*i : 4*i+4]
				want := []int32{bodies[i].px.Raw(), bodies[i].py.Raw(), bodies[i].vx.Raw(), bodies[i].vy.Raw()}
				for j := range want {
					if got[j] != want[j] {
						t.Fatalf("sub=%d frame=%d body=%d comp=%d: production=%d textbook=%d",
							sub, f, i, j, got[j], want[j])
					}
				}
			}
		}
	}
}
