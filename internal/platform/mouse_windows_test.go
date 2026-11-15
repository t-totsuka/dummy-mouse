package platform

import (
	"testing"

	"mousemover/internal/jiggle"
)

func TestIsOwnInput(t *testing.T) {
	commanded := jiggle.Point{X: 100, Y: 200}
	other := jiggle.Point{X: 101, Y: 200}

	tests := []struct {
		name      string
		remember  bool
		extraInfo uintptr
		injected  bool
		pt        jiggle.Point
		want      bool
	}{
		{"署名あり・注入", true, injectSignature, true, other, true},
		{"署名あり・注入フラグなし", true, injectSignature, false, other, true},
		{"署名あり・MoveTo 前", false, injectSignature, true, other, true},
		{"補正分（署名なしの注入・指示した座標）", true, 0, true, commanded, true},
		{"他人の注入入力（別の座標）", true, 0, true, other, false},
		{"他人の注入入力（別の署名）", true, 0x12345678, true, other, false},
		{"本物の入力（指示した座標）", true, 0, false, commanded, false},
		{"本物の入力（別の座標）", true, 0, false, other, false},
		{"MoveTo 前の注入入力", false, 0, true, jiggle.Point{}, false},
		{"MoveTo 前の注入入力（指示予定の座標）", false, 0, true, commanded, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMouse()
			if tt.remember {
				m.remember(commanded)
			}
			if got := m.IsOwnInput(tt.extraInfo, tt.injected, tt.pt); got != tt.want {
				t.Errorf("IsOwnInput(%#x, %v, %v) = %v, want %v", tt.extraInfo, tt.injected, tt.pt, got, tt.want)
			}
		})
	}
}

func TestIsOwnInputFollowsLatestCommand(t *testing.T) {
	m := NewMouse()
	first := jiggle.Point{X: 10, Y: 10}
	second := jiggle.Point{X: 20, Y: 10}
	m.remember(first)
	m.remember(second)
	if m.IsOwnInput(0, true, first) {
		t.Errorf("IsOwnInput at previous command = true, want false")
	}
	if !m.IsOwnInput(0, true, second) {
		t.Errorf("IsOwnInput at latest command = false, want true")
	}
}

func TestInjectSignature(t *testing.T) {
	if injectSignature != 0x4D4F5645 {
		t.Errorf("injectSignature = %#x, want 0x4D4F5645", injectSignature)
	}
}

func TestNormalize(t *testing.T) {
	single := jiggle.Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	multi := jiggle.Rect{Left: -1920, Top: -200, Right: 1920, Bottom: 880} // 3840x1080

	tests := []struct {
		name           string
		p              jiggle.Point
		b              jiggle.Rect
		wantDX, wantDY int32
	}{
		{"左上の端", jiggle.Point{X: 0, Y: 0}, single, 0, 0},
		{"右下の端", jiggle.Point{X: 1919, Y: 1079}, single, 65535, 65535},
		{"右上の端", jiggle.Point{X: 1919, Y: 0}, single, 65535, 0},
		{"負の原点・左上の端", jiggle.Point{X: -1920, Y: -200}, multi, 0, 0},
		{"負の原点・右下の端", jiggle.Point{X: 1919, Y: 879}, multi, 65535, 65535},
		{"範囲外は左上に収める", jiggle.Point{X: -5000, Y: -5000}, multi, 0, 0},
		{"範囲外は右下に収める", jiggle.Point{X: 5000, Y: 5000}, multi, 65535, 65535},
		{"幅 1 の画面", jiggle.Point{X: 7, Y: 3}, jiggle.Rect{Left: 7, Top: 3, Right: 8, Bottom: 4}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dx, dy := normalize(tt.p, tt.b)
			if dx != tt.wantDX || dy != tt.wantDY {
				t.Errorf("normalize(%v, %v) = (%d, %d), want (%d, %d)", tt.p, tt.b, dx, dy, tt.wantDX, tt.wantDY)
			}
		})
	}
}

func TestNormalizeCenter(t *testing.T) {
	for _, b := range []jiggle.Rect{
		{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		{Left: -1920, Top: -200, Right: 1920, Bottom: 880},
	} {
		w, h := b.Right-b.Left, b.Bottom-b.Top
		center := jiggle.Point{X: b.Left + w/2, Y: b.Top + h/2}
		dx, dy := normalize(center, b)
		if dx < 32700 || dx > 32800 || dy < 32700 || dy > 32800 {
			t.Errorf("normalize(center %v, %v) = (%d, %d), want ≈ (32767, 32767)", center, b, dx, dy)
		}
	}
	// 中央付近の 2 点（奇数の幅で厳密な中央）は 32767 / 32768 のどちらかになる。
	b := jiggle.Rect{Left: 0, Top: 0, Right: 1921, Bottom: 1081}
	if dx, dy := normalize(jiggle.Point{X: 960, Y: 540}, b); (dx != 32767 && dx != 32768) || (dy != 32767 && dy != 32768) {
		t.Errorf("normalize(exact center) = (%d, %d), want 32767 or 32768", dx, dy)
	}
}

// 正規化した座標を逆の式（dx * (width-1) / 65535 の丸め）で戻すと、元の画素に一致する（丸めで 1px ずれない）。
func TestNormalizeRoundTrip(t *testing.T) {
	b := jiggle.Rect{Left: -1920, Top: -200, Right: 1920, Bottom: 880}
	w := int64(b.Right - b.Left)
	for x := b.Left; x < b.Right; x++ {
		dx, _ := normalize(jiggle.Point{X: x, Y: b.Top}, b)
		back := b.Left + int((int64(dx)*(w-1)+65535/2)/65535)
		if back != x {
			t.Fatalf("x=%d: normalize -> %d -> %d", x, dx, back)
		}
	}
}

func TestMouseImplementsJiggleMouse(t *testing.T) {
	var _ jiggle.Mouse = NewMouse()
}

// 実機テスト: 実際のカーソルを 10px 動かし、元の位置に戻す。
func TestInteractiveMoveToAndBack(t *testing.T) {
	requireInteractive(t)
	m := NewMouse()
	b, err := m.Bounds()
	if err != nil {
		t.Fatalf("Bounds() = %v", err)
	}
	orig, err := m.CursorPos()
	if err != nil {
		t.Fatalf("CursorPos() = %v", err)
	}
	target := jiggle.Point{X: orig.X + 10, Y: orig.Y + 10}
	if target.X >= b.Right {
		target.X = orig.X - 10
	}
	if target.Y >= b.Bottom {
		target.Y = orig.Y - 10
	}
	if err := m.MoveTo(target); err != nil {
		t.Fatalf("MoveTo(%v) = %v", target, err)
	}
	if got, err := m.CursorPos(); err != nil || got != target {
		t.Errorf("CursorPos() after MoveTo = %v, %v; want %v", got, err, target)
	}
	if !m.IsOwnInput(0, true, target) {
		t.Errorf("IsOwnInput(0, true, %v) = false after MoveTo", target)
	}
	if err := m.MoveTo(orig); err != nil {
		t.Fatalf("MoveTo(orig %v) = %v", orig, err)
	}
	if got, err := m.CursorPos(); err != nil || got != orig {
		t.Errorf("CursorPos() after returning = %v, %v; want %v", got, err, orig)
	}
}
