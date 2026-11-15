package platform

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"mousemover/internal/jiggle"
)

// OwnInputFilter は、フックが受け取ったマウスイベントが自分の入力かどうかを判定する。
// *Mouse が実装する。
type OwnInputFilter interface {
	IsOwnInput(extraInfo uintptr, injected bool, pt jiggle.Point) bool
}

// stopTimeout は、Stop がフックのスレッドの終了を待つ上限。
const stopTimeout = 5 * time.Second

// Hook は、WH_MOUSE_LL の低レベルマウスフックと、その専用 OS スレッドを表す。
// プロセス内で同時に有効にできるフックは 1 つだけ。
type Hook struct {
	threadID uint32
	done     chan struct{} // スレッドが終了したら閉じる
	err      error         // done が閉じたあとに読む。メッセージループとフック解除のエラー

	stopOnce sync.Once
	stopErr  error
}

// hookState は、コールバックから参照する有効なフックの情報。
type hookState struct {
	onActivity func()
	filter     OwnInputFilter
}

var (
	// activeHook は有効なフック。コールバックはこれを通して通知先を見つける。
	activeHook atomic.Pointer[hookState]

	// hookCallback は SetWindowsHookExW に渡すコールバック。windows.NewCallback で作った
	// コールバックは解放されず作れる数にも上限があるため、プロセスで 1 回だけ作り、保持し続ける。
	hookCallbackOnce sync.Once
	hookCallback     uintptr
)

func lowLevelMouseCallback() uintptr {
	hookCallbackOnce.Do(func() {
		hookCallback = windows.NewCallback(lowLevelMouseProc)
	})
	return hookCallback
}

// lowLevelMouseProc は LowLevelMouseProc。フックのスレッドで呼ばれる。
// 判定と通知のあと、必ず CallNextHookEx を呼ぶ。
func lowLevelMouseProc(nCode, wParam, lParam uintptr) uintptr {
	if st := activeHook.Load(); st != nil {
		// lParam は OS が渡す MSLLHOOKSTRUCT のアドレス（Go のヒープ外）。uintptr から直接
		// unsafe.Pointer へ変換すると vet が警告するため、ポインターとして読み直す。
		st.dispatch(int32(nCode), wParam, *(**msllHookStruct)(unsafe.Pointer(&lParam)))
	}
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

// dispatch は、通知すべきイベントなら onActivity を呼ぶ。wParam（メッセージの種類）では
// 絞り込まない。移動、ボタン、ホイール、X ボタンのどれでも操作として数える。
func (st *hookState) dispatch(nCode int32, _ uintptr, info *msllHookStruct) {
	if shouldNotify(nCode, info, st.filter) {
		st.onActivity()
	}
}

// shouldNotify は、nCode == HC_ACTION で、filter が自分の入力ではないと判定したときだけ true を返す。
func shouldNotify(nCode int32, info *msllHookStruct, filter OwnInputFilter) bool {
	if nCode != hcAction || info == nil {
		return false
	}
	injected := info.flags&llmhfInjected != 0
	pt := jiggle.Point{X: int(info.pt.X), Y: int(info.pt.Y)}
	return !filter.IsOwnInput(info.dwExtraInfo, injected, pt)
}

// StartHook はフック専用の OS スレッドを起動し、WH_MOUSE_LL のフックを登録できるまで待つ。
// onActivity はフックのスレッドから呼ばれる。すぐに戻る処理にすること。
// 登録に失敗したとき、またはすでに有効なフックがあるときは error を返す。
// キーボードのフックは登録しない（要件 1.5）。
func StartHook(onActivity func(), filter interface {
	IsOwnInput(extraInfo uintptr, injected bool, pt jiggle.Point) bool
}) (*Hook, error) {
	if onActivity == nil || filter == nil {
		return nil, errors.New("フックの通知先または判定が指定されていません")
	}
	st := &hookState{onActivity: onActivity, filter: filter}
	if !activeHook.CompareAndSwap(nil, st) {
		return nil, errors.New("マウスフックはすでに開始されています")
	}
	callback := lowLevelMouseCallback()

	h := &Hook{done: make(chan struct{})}
	started := make(chan error, 1)
	go h.run(st, callback, started)
	if err := <-started; err != nil {
		<-h.done
		return nil, err
	}
	return h, nil
}

// run はフックのスレッドの本体。フックを登録し、メッセージループを回し、終了時にフックを外す。
func (h *Hook) run(st *hookState, callback uintptr, started chan<- error) {
	// UnlockOSThread は呼ばない。goroutine の終了とともに OS スレッドも終了させ、
	// UnhookWindowsHookEx が失敗した場合でも、スレッドの終了で OS にフックを外させるため。
	runtime.LockOSThread()
	defer close(h.done)
	defer activeHook.CompareAndSwap(st, nil)

	h.threadID = windows.GetCurrentThreadId()

	var hMod windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &hMod); err != nil {
		started <- fmt.Errorf("モジュールハンドルの取得に失敗しました: %w", err)
		return
	}
	hhk, _, callErr := procSetWindowsHookExW.Call(whMouseLL, callback, uintptr(hMod), 0)
	if hhk == 0 {
		started <- fmt.Errorf("マウスフックの登録に失敗しました: %w", lastError(callErr))
		return
	}
	started <- nil

	var loopErr error
	var m msg
	for {
		r, _, callErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == 0 { // WM_QUIT
			break
		}
		if int32(r) == -1 {
			loopErr = fmt.Errorf("メッセージループでエラーが起きました: %w", lastError(callErr))
			break
		}
	}

	var unhookErr error
	if r, _, callErr := procUnhookWindowsHookEx.Call(hhk); r == 0 {
		unhookErr = fmt.Errorf("マウスフックの解除に失敗しました: %w", lastError(callErr))
	}
	h.err = errors.Join(loopErr, unhookErr)
}

// Stop は、フックのスレッドに WM_QUIT を送り、フックを外してスレッドが終わるのを待つ。
// stopTimeout を過ぎても終わらないときは error を返す。
// 2 回目以降の呼び出しは何もせず、1 回目と同じ結果を返す。nil の *Hook では nil を返す。
func (h *Hook) Stop() error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() { h.stopErr = h.stop() })
	return h.stopErr
}

func (h *Hook) stop() error {
	select {
	case <-h.done: // メッセージループがすでに終わっている
		return h.err
	default:
	}
	deadline := time.After(stopTimeout)
	for {
		r, _, callErr := procPostThreadMessageW.Call(uintptr(h.threadID), wmQuit, 0, 0)
		if r != 0 {
			break
		}
		// スレッドがすでに終わっていれば、送れなくても問題ない
		select {
		case <-h.done:
			return h.err
		case <-deadline:
			return fmt.Errorf("フックのスレッドに終了を通知できませんでした: %w", lastError(callErr))
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-h.done:
		return h.err
	case <-deadline:
		return errors.New("フックのスレッドが時間内に終了しませんでした")
	}
}
