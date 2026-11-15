package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// fallbackText は、文字列を UTF-16 に変換できなかったとき（NUL を含むなど）に使う。
const fallbackText = "エラーが発生しました。"

// ShowError は、OK ボタンとエラーアイコンのメッセージボックスを表示し、閉じられるまで待つ。
// 表示自体に失敗しても、ほかに伝える手段がないため何もしない。
func ShowError(title, message string) {
	if err := procMessageBoxW.Find(); err != nil {
		return
	}
	t := utf16PtrOrFallback(title)
	m := utf16PtrOrFallback(message)
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbOK|mbIconError)
}

// utf16PtrOrFallback は s を NUL 終端の UTF-16 に変換する。s が NUL を含むなどで
// 変換できないときは fallbackText を返す。
func utf16PtrOrFallback(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString(fallbackText)
	}
	return p
}
