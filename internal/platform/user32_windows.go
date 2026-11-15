package platform

import "golang.org/x/sys/windows"

// user32.dll の関数。NewLazySystemDLL は System32 からのみ読み込むため、DLL の差し替えを防げる。
// 関数アドレスは初回の Call / Find で解決される。
var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procSendInput                     = user32.NewProc("SendInput")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procSetCursorPos                  = user32.NewProc("SetCursorPos")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procSetWindowsHookExW             = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx           = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx                = user32.NewProc("CallNextHookEx")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procPostThreadMessageW            = user32.NewProc("PostThreadMessageW")
	procMessageBoxW                   = user32.NewProc("MessageBoxW")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
)

// 定数（winuser.h / windef.h）。呼び出し側の型に合わせやすいよう型なし定数にしている。
const (
	inputMouse = 0 // INPUT_MOUSE

	mouseeventfMove        = 0x0001 // MOUSEEVENTF_MOVE
	mouseeventfVirtualDesk = 0x4000 // MOUSEEVENTF_VIRTUALDESK
	mouseeventfAbsolute    = 0x8000 // MOUSEEVENTF_ABSOLUTE

	smXVirtualScreen  = 76 // SM_XVIRTUALSCREEN
	smYVirtualScreen  = 77 // SM_YVIRTUALSCREEN
	smCXVirtualScreen = 78 // SM_CXVIRTUALSCREEN
	smCYVirtualScreen = 79 // SM_CYVIRTUALSCREEN

	whMouseLL     = 14     // WH_MOUSE_LL
	hcAction      = 0      // HC_ACTION
	wmQuit        = 0x0012 // WM_QUIT
	llmhfInjected = 0x0001 // LLMHF_INJECTED

	mbOK        = 0x00000000 // MB_OK
	mbIconError = 0x00000010 // MB_ICONERROR
)

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 は ((DPI_AWARENESS_CONTEXT)-4)。
const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

// point は POINT。
type point struct {
	X, Y int32
}

// mouseInput は MOUSEINPUT。
type mouseInput struct {
	dx, dy      int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input は INPUT（type が INPUT_MOUSE の場合）。共用体のうち最大のメンバーは MOUSEINPUT なので、
// mi だけを置けばサイズが一致する。type の後のパディングは uintptr のアラインメントで自動的に入る
// （amd64 では合計 40 バイト）。
type input struct {
	inputType uint32
	mi        mouseInput
}

// msllHookStruct は MSLLHOOKSTRUCT。
type msllHookStruct struct {
	pt          point
	mouseData   uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

// msg は MSG。
type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}
