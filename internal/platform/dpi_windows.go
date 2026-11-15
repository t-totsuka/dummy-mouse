package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// EnableDPIAwareness は、プロセスを Per-Monitor DPI Aware V2 にする。
// 物理ピクセル単位でカーソルを動かし、フックの座標と GetCursorPos の座標をそろえるために使う。
// すでに DPI の設定がされている場合（manifest や 2 回目の呼び出し）は ERROR_ACCESS_DENIED を
// 包んだエラーを返す。呼び出し側は失敗しても続行してよい。
func EnableDPIAwareness() error {
	if err := procSetProcessDpiAwarenessContext.Find(); err != nil {
		return fmt.Errorf("SetProcessDpiAwarenessContext が見つかりません: %w", err)
	}
	r, _, callErr := procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2)
	if r == 0 {
		return fmt.Errorf("DPI の設定に失敗しました: %w", lastError(callErr))
	}
	return nil
}

// errNoErrorCode は、関数が失敗を返したのに GetLastError が 0 だったときに使う。
var errNoErrorCode = errors.New("エラーコードなし")

// lastError は、LazyProc.Call が返した GetLastError の値を error にする。
// 値が 0 のときも nil にはせず errNoErrorCode を返す。
func lastError(err error) error {
	var errno windows.Errno
	if err == nil || (errors.As(err, &errno) && errno == 0) {
		return errNoErrorCode
	}
	return err
}
