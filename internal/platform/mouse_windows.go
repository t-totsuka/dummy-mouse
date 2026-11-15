package platform

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"mousemover/internal/jiggle"
)

// injectSignature は、自分が SendInput で送った入力の dwExtraInfo に付ける署名（"MOVE"）。
const injectSignature uintptr = 0x4D4F5645

// normalizedMax は MOUSEEVENTF_ABSOLUTE の座標の最大値。
const normalizedMax = 65535

// Mouse は jiggle.Mouse を Win32 で実装し、自分の入力を見分ける。
// lastCommanded はフックのスレッドからも読まれるため、ロックを使わずに atomic で持つ。
type Mouse struct {
	lastCommanded atomic.Pointer[jiggle.Point] // nil は「まだ指示していない」
}

var _ jiggle.Mouse = (*Mouse)(nil)

// NewMouse は Mouse を作る。
func NewMouse() *Mouse { return &Mouse{} }

// CursorPos は GetCursorPos で現在のカーソル位置を返す。
func (m *Mouse) CursorPos() (jiggle.Point, error) {
	var pt point
	r, _, callErr := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if r == 0 {
		return jiggle.Point{}, fmt.Errorf("カーソル位置の取得に失敗しました: %w", lastError(callErr))
	}
	return jiggle.Point{X: int(pt.X), Y: int(pt.Y)}, nil
}

// Bounds は仮想スクリーン（すべてのモニターを含む範囲）を返す。
func (m *Mouse) Bounds() (jiggle.Rect, error) {
	x := systemMetric(smXVirtualScreen)
	y := systemMetric(smYVirtualScreen)
	cx := systemMetric(smCXVirtualScreen)
	cy := systemMetric(smCYVirtualScreen)
	if cx <= 0 || cy <= 0 {
		return jiggle.Rect{}, fmt.Errorf("仮想スクリーンの大きさを取得できません (%dx%d)", cx, cy)
	}
	return jiggle.Rect{Left: x, Top: y, Right: x + cx, Bottom: y + cy}, nil
}

// systemMetric は GetSystemMetrics の戻り値（int）を返す。失敗時は 0 になる。
func systemMetric(index int) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(int32(r))
}

// MoveTo は、署名付きの絶対座標の SendInput で p へ移動する。
// 着地した位置が p と違えば SetCursorPos で補正する。
func (m *Mouse) MoveTo(p jiggle.Point) error {
	b, err := m.Bounds()
	if err != nil {
		return err
	}
	m.remember(p)

	dx, dy := normalize(p, b)
	in := input{
		inputType: inputMouse,
		mi: mouseInput{
			dx:          dx,
			dy:          dy,
			dwFlags:     mouseeventfMove | mouseeventfAbsolute | mouseeventfVirtualDesk,
			dwExtraInfo: injectSignature,
		},
	}
	r, _, callErr := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if r == 0 {
		return fmt.Errorf("マウス入力の送信に失敗しました: %w", lastError(callErr))
	}

	got, err := m.CursorPos()
	if err != nil {
		return err
	}
	if got == p {
		return nil
	}
	// 正規化の丸めなどでずれた分を補正する。この補正によるフックのイベントは、
	// 「最後に指示した座標への注入された入力」として IsOwnInput で除外される。
	r, _, callErr = procSetCursorPos.Call(uintptr(int32(p.X)), uintptr(int32(p.Y)))
	if r == 0 {
		return fmt.Errorf("カーソル位置の補正に失敗しました: %w", lastError(callErr))
	}
	got, err = m.CursorPos()
	if err != nil {
		return err
	}
	if got != p {
		return fmt.Errorf("カーソルが指定した位置に移動しませんでした (指定 %d,%d / 実際 %d,%d)", p.X, p.Y, got.X, got.Y)
	}
	return nil
}

// remember は最後に指示した座標を記録する。
func (m *Mouse) remember(p jiggle.Point) {
	m.lastCommanded.Store(&p)
}

// IsOwnInput はフックから呼ばれる。次のどちらかなら true を返す。
// (1) extraInfo == injectSignature
// (2) injected かつ pt == 最後に指示した座標（SetCursorPos による補正の分）
func (m *Mouse) IsOwnInput(extraInfo uintptr, injected bool, pt jiggle.Point) bool {
	if extraInfo == injectSignature {
		return true
	}
	if !injected {
		return false
	}
	last := m.lastCommanded.Load()
	return last != nil && *last == pt
}

// normalize は仮想スクリーン座標 p を、MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK 用の
// 0–65535 の座標にする。端の画素がちょうど 0 と 65535 になるよう (幅-1) で割り、最も近い値に丸める。
func normalize(p jiggle.Point, b jiggle.Rect) (dx, dy int32) {
	return normalizeAxis(p.X, b.Left, b.Right-b.Left), normalizeAxis(p.Y, b.Top, b.Bottom-b.Top)
}

func normalizeAxis(v, origin, size int) int32 {
	span := int64(size) - 1
	if span <= 0 {
		return 0
	}
	n := (int64(v-origin)*normalizedMax + span/2) / span
	if n < 0 {
		return 0
	}
	if n > normalizedMax {
		return normalizedMax
	}
	return int32(n)
}
