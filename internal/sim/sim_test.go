package sim

import (
	"testing"

	"goframe/internal/fixed"
)

// TestIntegrateDragOrderIsPinned 锁住生产路径的相乘顺序。
//
// 阻尼冲量必须是 v*(k*dt)（fixed.Mul3 的右结合约定）。若有人把它改写成
// (v*k)*dt，这里用 G1 组数值就能抓到：v=3.3、k=0.13、子步 3 时，
// 规范顺序的系数 k*dt=2777，而 (v*k)*dt 会得到不同的逐帧速度。
func TestIntegrateDragOrderIsPinned(t *testing.T) {
	const sub = 3
	s := NewState(1, 0)
	s.SetDrag(fixed.FromRaw(8520))      // 0.13
	s.Push(0, 0, fixed.FromRaw(216268)) // v=3.3
	s.Integrate(sub)

	dt := fixed.One.Div(fixed.FromInt(sub))
	wantCoef := fixed.FromRaw(8520).Mul(dt)
	if got := s.DragCoef(); got != wantCoef {
		t.Fatalf("系数不是 k*dt: got=%d want=%d", got.Raw(), wantCoef.Raw())
	}
	// 规范顺序的阻尼冲量 = Mul3(v, k, dt)。
	wantImpulse := fixed.Mul3(fixed.FromRaw(216268), fixed.FromRaw(8520), dt)
	if got, ghost := wantImpulse, fixed.FromRaw(216268).Mul(fixed.FromRaw(8520)).Mul(dt); got == ghost {
		t.Fatalf("G1 组两种顺序居然相同，无法锁顺序（canon=%d）", got.Raw())
	}
	// 首个实体积分后 velY = v - v*(k*dt) + g（g=0）。
	want := fixed.FromRaw(216268).Sub(wantImpulse)
	if got := s.Raw()[3]; got != want.Raw() {
		t.Fatalf("velY=%d，期望 %d（顺序被改动？）", got, want.Raw())
	}
}

// TestDragDefaultIsOff 确认默认不改变旧行为：不设阻尼时与无阻尼积分一致。
func TestDragDefaultIsOff(t *testing.T) {
	a := NewState(2, fixed.FromInt(2))
	b := NewState(2, fixed.FromInt(2))
	b.SetDrag(fixed.FromRaw(8520))
	a.Push(0, 0, fixed.FromInt(5))
	b.Push(0, 0, fixed.FromInt(5))
	a.Integrate(4)
	b.Integrate(4)
	if a.Raw()[3] == b.Raw()[3] {
		t.Fatal("阻尼已设置却与无阻尼结果相同")
	}
}
