package platform

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// ShowError はモーダルなダイアログを出してブロックするため、
// MOUSEMOVER_INTERACTIVE=1 に加えて MOUSEMOVER_DIALOG=1 のときだけ実行する。
func TestInteractiveShowError(t *testing.T) {
	requireInteractive(t)
	if os.Getenv("MOUSEMOVER_DIALOG") != "1" {
		t.Skip("set MOUSEMOVER_DIALOG=1 to show a real message box")
	}
	ShowError("mousemover テスト", "これはテスト用のエラーダイアログです。OK を押してください。")
}

func TestUTF16PtrOrFallback(t *testing.T) {
	if got := windows.UTF16PtrToString(utf16PtrOrFallback("エラー")); got != "エラー" {
		t.Errorf("utf16PtrOrFallback(エラー) = %q", got)
	}
	if got := windows.UTF16PtrToString(utf16PtrOrFallback("a\x00b")); got != fallbackText {
		t.Errorf("utf16PtrOrFallback with NUL = %q, want %q", got, fallbackText)
	}
}
