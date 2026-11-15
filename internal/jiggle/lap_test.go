package jiggle

import "testing"

func TestLapTargets(t *testing.T) {
	screen := Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	tests := []struct {
		name   string
		start  Point
		step   int
		bounds Rect
		want   [4]Point
	}{
		{
			name:   "通常の位置: 上 → 右上 → 右 → 開始位置",
			start:  Point{100, 100},
			step:   10,
			bounds: screen,
			want:   [4]Point{{100, 90}, {110, 90}, {110, 100}, {100, 100}},
		},
		{
			name:   "上端: 上へは動かず横へは動く",
			start:  Point{100, 0},
			step:   10,
			bounds: screen,
			want:   [4]Point{{100, 0}, {110, 0}, {110, 0}, {100, 0}},
		},
		{
			name:   "上端の近く: 上は Top で止まる",
			start:  Point{100, 4},
			step:   10,
			bounds: screen,
			want:   [4]Point{{100, 0}, {110, 0}, {110, 4}, {100, 4}},
		},
		{
			name:   "左端: 右方向にしか動かないので範囲内のまま",
			start:  Point{0, 500},
			step:   10,
			bounds: screen,
			want:   [4]Point{{0, 490}, {10, 490}, {10, 500}, {0, 500}},
		},
		{
			name:   "右下の角: 右は Right-1 で止まる",
			start:  Point{1919, 1079},
			step:   10,
			bounds: screen,
			want:   [4]Point{{1919, 1069}, {1919, 1069}, {1919, 1079}, {1919, 1079}},
		},
		{
			name:   "右端の近く: 右は Right-1 で止まる",
			start:  Point{1915, 500},
			step:   10,
			bounds: screen,
			want:   [4]Point{{1915, 490}, {1919, 490}, {1919, 500}, {1915, 500}},
		},
		{
			name:   "右上の角: 上にも右にも動かない",
			start:  Point{1919, 0},
			step:   10,
			bounds: screen,
			want:   [4]Point{{1919, 0}, {1919, 0}, {1919, 0}, {1919, 0}},
		},
		{
			name:   "負の原点を持つ仮想スクリーン: 通常の位置",
			start:  Point{-1000, -200},
			step:   10,
			bounds: Rect{Left: -1920, Top: -300, Right: 1920, Bottom: 1080},
			want:   [4]Point{{-1000, -210}, {-990, -210}, {-990, -200}, {-1000, -200}},
		},
		{
			name:   "負の原点を持つ仮想スクリーン: 左上の角",
			start:  Point{-1920, -300},
			step:   10,
			bounds: Rect{Left: -1920, Top: -300, Right: 1920, Bottom: 1080},
			want:   [4]Point{{-1920, -300}, {-1910, -300}, {-1910, -300}, {-1920, -300}},
		},
		{
			name:   "負の原点を持つ仮想スクリーン: 右端は Right-1（負の値）",
			start:  Point{-5, 50},
			step:   10,
			bounds: Rect{Left: -1920, Top: 0, Right: 0, Bottom: 1080},
			want:   [4]Point{{-5, 40}, {-1, 40}, {-1, 50}, {-5, 50}},
		},
		{
			name:   "開始位置が範囲外: 収めた開始位置に戻る",
			start:  Point{2500, -50},
			step:   10,
			bounds: screen,
			want:   [4]Point{{1919, 0}, {1919, 0}, {1919, 0}, {1919, 0}},
		},
		{
			name:   "開始位置が左下の範囲外: 収めた開始位置を基準にする",
			start:  Point{-30, 2000},
			step:   10,
			bounds: screen,
			want:   [4]Point{{0, 1069}, {10, 1069}, {10, 1079}, {0, 1079}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LapTargets(tt.start, tt.step, tt.bounds)
			if got != tt.want {
				t.Errorf("LapTargets(%v, %d, %v) = %v, want %v", tt.start, tt.step, tt.bounds, got, tt.want)
			}
			// 本体の補助関数を使わず、テスト側で独立に収めた開始位置を求める。
			wantStart := tt.start
			wantStart.X = max(tt.bounds.Left, min(wantStart.X, tt.bounds.Right-1))
			wantStart.Y = max(tt.bounds.Top, min(wantStart.Y, tt.bounds.Bottom-1))
			if got[3] != wantStart {
				t.Errorf("4 番目 = %v, want 開始位置 %v", got[3], wantStart)
			}
			for i, p := range got {
				if p.X < tt.bounds.Left || p.X >= tt.bounds.Right || p.Y < tt.bounds.Top || p.Y >= tt.bounds.Bottom {
					t.Errorf("目標 %d = %v が範囲 %v の外にある", i, p, tt.bounds)
				}
			}
		})
	}
}
