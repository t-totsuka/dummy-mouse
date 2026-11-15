package platform

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

// テストプロセスは manifest で DPI 設定をしていないため、初回呼び出しは成功するはず。
// 2 回目以降は ERROR_ACCESS_DENIED になる（本番の main は失敗しても続行する設計）。
func TestInteractiveEnableDPIAwareness(t *testing.T) {
	requireInteractive(t)
	if err := EnableDPIAwareness(); err != nil {
		t.Fatalf("EnableDPIAwareness() = %v, want nil", err)
	}
}

func TestLastError(t *testing.T) {
	if got := lastError(nil); !errors.Is(got, errNoErrorCode) {
		t.Errorf("lastError(nil) = %v", got)
	}
	if got := lastError(windows.Errno(0)); !errors.Is(got, errNoErrorCode) {
		t.Errorf("lastError(Errno(0)) = %v", got)
	}
	if got := lastError(windows.ERROR_ACCESS_DENIED); !errors.Is(got, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("lastError(ERROR_ACCESS_DENIED) = %v", got)
	}
}
