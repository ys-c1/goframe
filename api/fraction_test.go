package api

import "testing"

func TestFractionalGravity98(t *testing.T) {
	// SubSteps=1、零初值下，一帧后 velY 与 posY 都等于重力原始分子。
	w, err := New(Options{Bodies: 1, SubSteps: 1, GravityNum: 49, GravityDen: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Step(nil); err != nil {
		t.Fatal(err)
	}
	snap := w.Snapshot() // 顺序：posX, posY, velX, velY
	if snap[3] != 642253 || snap[1] != 642253 {
		t.Fatalf("9.8 一帧后 posY/velY=%d/%d，期望 642253", snap[1], snap[3])
	}

	// GravityDen>0 优先于整数字段；Den==0 时保持旧行为。
	w2, err := New(Options{Bodies: 1, SubSteps: 1, Gravity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := w2.Step(nil); err != nil {
		t.Fatal(err)
	}
	if got := w2.Snapshot()[3]; got != 2<<16 {
		t.Fatalf("整数重力 velY raw=%d 期望 %d", got, 2<<16)
	}

	if _, err := New(Options{Bodies: 1, SubSteps: 1, GravityDen: -3}); err == nil {
		t.Fatal("负分母应报错")
	}
}
