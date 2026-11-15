package platform

import (
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"mousemover/internal/jiggle"
)

// fakeFilter は IsOwnInput の呼び出しを記録し、決めた値を返す。
type fakeFilter struct {
	own       bool
	called    bool
	extraInfo uintptr
	injected  bool
	pt        jiggle.Point
}

func (f *fakeFilter) IsOwnInput(extraInfo uintptr, injected bool, pt jiggle.Point) bool {
	f.called = true
	f.extraInfo = extraInfo
	f.injected = injected
	f.pt = pt
	return f.own
}

func TestShouldNotify(t *testing.T) {
	commanded := jiggle.Point{X: 100, Y: 200}

	tests := []struct {
		name  string
		nCode int32
		info  msllHookStruct
		want  bool
	}{
		{"nCode が負", -1, msllHookStruct{pt: point{1, 2}}, false},
		{"nCode が HC_ACTION 以外の正の値", 3, msllHookStruct{pt: point{1, 2}}, false},
		{"本物の入力", hcAction, msllHookStruct{pt: point{1, 2}}, true},
		{"本物の入力（指示した座標）", hcAction, msllHookStruct{pt: point{100, 200}}, true},
		{"署名付きの入力", hcAction, msllHookStruct{pt: point{5, 5}, flags: llmhfInjected, dwExtraInfo: injectSignature}, false},
		{"補正分（指示した座標への注入）", hcAction, msllHookStruct{pt: point{100, 200}, flags: llmhfInjected}, false},
		{"他のソフトの注入入力", hcAction, msllHookStruct{pt: point{101, 200}, flags: llmhfInjected}, true},
		{"他のソフトの注入入力（別の署名）", hcAction, msllHookStruct{pt: point{100, 200}, flags: llmhfInjected | 0x2, dwExtraInfo: 0x1234}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMouse()
			m.remember(commanded)
			info := tt.info
			if got := shouldNotify(tt.nCode, &info, m); got != tt.want {
				t.Errorf("shouldNotify(%d, %+v) = %v, want %v", tt.nCode, tt.info, got, tt.want)
			}
		})
	}
}

func TestShouldNotifyPassesFieldsToFilter(t *testing.T) {
	f := &fakeFilter{}
	info := msllHookStruct{pt: point{-30, 40}, flags: llmhfInjected, dwExtraInfo: 0xABCD}
	if !shouldNotify(hcAction, &info, f) {
		t.Fatalf("shouldNotify = false, want true when filter says not own")
	}
	if !f.called || f.extraInfo != 0xABCD || !f.injected || f.pt != (jiggle.Point{X: -30, Y: 40}) {
		t.Errorf("filter got (%#x, %v, %v), want (0xabcd, true, {-30 40})", f.extraInfo, f.injected, f.pt)
	}

	f = &fakeFilter{own: true}
	if shouldNotify(hcAction, &info, f) {
		t.Errorf("shouldNotify = true, want false when filter says own")
	}

	f = &fakeFilter{}
	info = msllHookStruct{flags: 0x2} // LLMHF_LOWER_IL_INJECTED だけでは注入扱いにしない
	shouldNotify(hcAction, &info, f)
	if f.injected {
		t.Errorf("injected = true for flags 0x2, want false")
	}

	f = &fakeFilter{}
	shouldNotify(-1, &info, f)
	if f.called {
		t.Errorf("filter called for nCode < 0")
	}
}

// TestHookCallbackIgnoresWParam は、移動・ボタン・ホイール・X ボタンのどのメッセージでも
// 同じように通知することを確かめる（wParam で絞り込まない）。CallNextHookEx を呼ばないよう、
// コールバック本体ではなく判定を通知につなぐ部分（dispatch）を使う。
func TestHookCallbackIgnoresWParam(t *testing.T) {
	messages := map[string]uintptr{
		"WM_MOUSEMOVE":   0x0200,
		"WM_LBUTTONDOWN": 0x0201,
		"WM_LBUTTONUP":   0x0202,
		"WM_RBUTTONDOWN": 0x0204,
		"WM_MBUTTONDOWN": 0x0207,
		"WM_MOUSEWHEEL":  0x020A,
		"WM_XBUTTONDOWN": 0x020B,
		"WM_MOUSEHWHEEL": 0x020E,
		"未知の値":           0xFFFF,
	}
	for name, wParam := range messages {
		t.Run(name, func(t *testing.T) {
			var n int
			st := &hookState{onActivity: func() { n++ }, filter: NewMouse()}
			info := msllHookStruct{pt: point{1, 1}}
			st.dispatch(hcAction, wParam, &info)
			if n != 1 {
				t.Errorf("onActivity called %d times for %s, want 1", n, name)
			}
		})
	}
}

func TestHookStopNil(t *testing.T) {
	var h *Hook
	if err := h.Stop(); err != nil {
		t.Errorf("(*Hook)(nil).Stop() = %v, want nil", err)
	}
}

// --- 実機テスト（MOUSEMOVER_INTERACTIVE=1 のときだけ動く） ---

func TestInteractiveHookStartStopRepeated(t *testing.T) {
	requireInteractive(t)
	for i := 0; i < 3; i++ {
		h, err := StartHook(func() {}, NewMouse())
		if err != nil {
			t.Fatalf("StartHook #%d = %v", i, err)
		}
		if _, err := StartHook(func() {}, NewMouse()); err == nil {
			t.Errorf("second StartHook while active succeeded, want error")
		}
		if err := h.Stop(); err != nil {
			t.Fatalf("Stop #%d = %v", i, err)
		}
		if err := h.Stop(); err != nil {
			t.Errorf("second Stop #%d = %v, want nil", i, err)
		}
	}
}

func TestInteractiveHookNotifications(t *testing.T) {
	requireInteractive(t)
	m := NewMouse()
	orig, err := m.CursorPos()
	if err != nil {
		t.Fatalf("CursorPos = %v", err)
	}
	t.Cleanup(func() {
		procSetCursorPos.Call(uintptr(int32(orig.X)), uintptr(int32(orig.Y)))
	})

	var count atomic.Int32
	h, err := StartHook(func() { count.Add(1) }, m)
	if err != nil {
		t.Fatalf("StartHook = %v", err)
	}
	defer h.Stop()

	// 署名なしの注入入力（他のソフト扱い）→ 通知が来る
	in := input{inputType: inputMouse, mi: mouseInput{dx: 1, dwFlags: mouseeventfMove}}
	if r, _, callErr := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in)); r == 0 {
		t.Fatalf("SendInput = %v", callErr)
	}
	if !waitFor(func() bool { return count.Load() > 0 }, 2*time.Second) {
		t.Errorf("no activity notified for unsigned injected input")
	}

	// 署名付きの移動 → 通知が来ない
	time.Sleep(200 * time.Millisecond)
	count.Store(0)
	target := orig
	if target.X > 0 {
		target.X--
	} else {
		target.X++
	}
	if err := m.MoveTo(target); err != nil {
		t.Fatalf("MoveTo = %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := count.Load(); n != 0 {
		t.Errorf("activity notified %d times for signed MoveTo, want 0", n)
	}
}

func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
