package jiggle

// LapTargets は start から上 → 右 → 下 → 左の順に step ずつ動くときの 4 つの目標座標を返す。
// 戻り値: [上, 右上, 右, 開始位置]。各座標は bounds に収めた値。
// 4 番目は必ず start（start が bounds の外にある場合は、収めた start）。
func LapTargets(start Point, step int, bounds Rect) [4]Point {
	s := clampPoint(start, bounds)
	return [4]Point{
		clampPoint(Point{s.X, s.Y - step}, bounds),
		clampPoint(Point{s.X + step, s.Y - step}, bounds),
		clampPoint(Point{s.X + step, s.Y}, bounds),
		s,
	}
}

// clampPoint は p を bounds の中に収める。Right と Bottom は範囲に含まないので、最大値はそれぞれ Right-1 と Bottom-1。
func clampPoint(p Point, bounds Rect) Point {
	return Point{
		X: clampInt(p.X, bounds.Left, bounds.Right-1),
		Y: clampInt(p.Y, bounds.Top, bounds.Bottom-1),
	}
}

// clampInt は v を [lo, hi] に収める。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
