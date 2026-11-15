package platform

import (
	"os"
	"runtime"
	"testing"
	"unsafe"
)

func TestStructLayoutAMD64(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skipf("layout values are for amd64; GOARCH=%s", runtime.GOARCH)
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"sizeof(point)", unsafe.Sizeof(point{}), 8},
		{"sizeof(mouseInput)", unsafe.Sizeof(mouseInput{}), 32},
		{"offsetof(mouseInput.mouseData)", unsafe.Offsetof(mouseInput{}.mouseData), 8},
		{"offsetof(mouseInput.dwFlags)", unsafe.Offsetof(mouseInput{}.dwFlags), 12},
		{"offsetof(mouseInput.time)", unsafe.Offsetof(mouseInput{}.time), 16},
		{"offsetof(mouseInput.dwExtraInfo)", unsafe.Offsetof(mouseInput{}.dwExtraInfo), 24},
		{"sizeof(input)", unsafe.Sizeof(input{}), 40},
		{"offsetof(input.mi)", unsafe.Offsetof(input{}.mi), 8},
		{"sizeof(msllHookStruct)", unsafe.Sizeof(msllHookStruct{}), 32},
		{"offsetof(msllHookStruct.mouseData)", unsafe.Offsetof(msllHookStruct{}.mouseData), 8},
		{"offsetof(msllHookStruct.flags)", unsafe.Offsetof(msllHookStruct{}.flags), 12},
		{"offsetof(msllHookStruct.time)", unsafe.Offsetof(msllHookStruct{}.time), 16},
		{"offsetof(msllHookStruct.dwExtraInfo)", unsafe.Offsetof(msllHookStruct{}.dwExtraInfo), 24},
		{"sizeof(msg)", unsafe.Sizeof(msg{}), 48},
		{"offsetof(msg.message)", unsafe.Offsetof(msg{}.message), 8},
		{"offsetof(msg.wParam)", unsafe.Offsetof(msg{}.wParam), 16},
		{"offsetof(msg.lParam)", unsafe.Offsetof(msg{}.lParam), 24},
		{"offsetof(msg.time)", unsafe.Offsetof(msg{}.time), 32},
		{"offsetof(msg.pt)", unsafe.Offsetof(msg{}.pt), 36},
		{"offsetof(msg.lPrivate)", unsafe.Offsetof(msg{}.lPrivate), 44},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestConstants(t *testing.T) {
	cases := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"inputMouse", inputMouse, 0},
		{"mouseeventfMove", mouseeventfMove, 0x0001},
		{"mouseeventfAbsolute", mouseeventfAbsolute, 0x8000},
		{"mouseeventfVirtualDesk", mouseeventfVirtualDesk, 0x4000},
		{"smXVirtualScreen", smXVirtualScreen, 76},
		{"smYVirtualScreen", smYVirtualScreen, 77},
		{"smCXVirtualScreen", smCXVirtualScreen, 78},
		{"smCYVirtualScreen", smCYVirtualScreen, 79},
		{"whMouseLL", whMouseLL, 14},
		{"hcAction", hcAction, 0},
		{"wmQuit", wmQuit, 0x0012},
		{"llmhfInjected", llmhfInjected, 0x00000001},
		{"mbOK", mbOK, 0x00000000},
		{"mbIconError", mbIconError, 0x00000010},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 は ((DPI_AWARENESS_CONTEXT)-4)
	if dpiAwarenessContextPerMonitorAwareV2 != ^uintptr(3) {
		t.Errorf("dpiAwarenessContextPerMonitorAwareV2 = %#x, want -4 as uintptr", dpiAwarenessContextPerMonitorAwareV2)
	}
}

// user32.dll の読み込みと関数アドレスの解決だけを行う（呼び出しはしない）。
func TestProcsResolve(t *testing.T) {
	procs := map[string]interface{ Find() error }{
		"SendInput":                     procSendInput,
		"GetCursorPos":                  procGetCursorPos,
		"SetCursorPos":                  procSetCursorPos,
		"GetSystemMetrics":              procGetSystemMetrics,
		"SetWindowsHookExW":             procSetWindowsHookExW,
		"UnhookWindowsHookEx":           procUnhookWindowsHookEx,
		"CallNextHookEx":                procCallNextHookEx,
		"GetMessageW":                   procGetMessageW,
		"PostThreadMessageW":            procPostThreadMessageW,
		"MessageBoxW":                   procMessageBoxW,
		"SetProcessDpiAwarenessContext": procSetProcessDpiAwarenessContext,
	}
	for name, p := range procs {
		if err := p.Find(); err != nil {
			t.Errorf("%s: Find() = %v", name, err)
		}
	}
}

func requireInteractive(t *testing.T) {
	t.Helper()
	if os.Getenv("MOUSEMOVER_INTERACTIVE") != "1" {
		t.Skip("set MOUSEMOVER_INTERACTIVE=1 to run desktop-dependent tests")
	}
}

func TestInteractiveVirtualScreenSize(t *testing.T) {
	requireInteractive(t)
	cx, _, _ := procGetSystemMetrics.Call(uintptr(smCXVirtualScreen))
	cy, _, _ := procGetSystemMetrics.Call(uintptr(smCYVirtualScreen))
	if int32(cx) <= 0 || int32(cy) <= 0 {
		t.Fatalf("virtual screen size = %dx%d, want positive", int32(cx), int32(cy))
	}
}
