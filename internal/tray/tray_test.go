package tray

import (
	"bytes"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"
)

func TestIconIsICOWith16And32(t *testing.T) {
	if len(iconBytes) < 6 {
		t.Fatalf("icon too short: %d bytes", len(iconBytes))
	}
	if !bytes.Equal(iconBytes[:4], []byte{0, 0, 1, 0}) {
		t.Fatalf("icon header = % x, want 00 00 01 00", iconBytes[:4])
	}
	count := int(binary.LittleEndian.Uint16(iconBytes[4:6]))
	if count < 2 {
		t.Fatalf("image count = %d, want >= 2", count)
	}
	if len(iconBytes) < 6+16*count {
		t.Fatalf("icon directory truncated")
	}
	sizes := map[int]bool{}
	for i := 0; i < count; i++ {
		e := iconBytes[6+16*i : 6+16*(i+1)]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		size := binary.LittleEndian.Uint32(e[8:12])
		offset := binary.LittleEndian.Uint32(e[12:16])
		if size == 0 || int(offset)+int(size) > len(iconBytes) {
			t.Fatalf("entry %d: size=%d offset=%d out of range (len=%d)", i, size, offset, len(iconBytes))
		}
		sizes[w] = true
	}
	for _, want := range []int{16, 32} {
		if !sizes[want] {
			t.Errorf("icon has no %dx%d entry (have %v)", want, want, sizes)
		}
	}
}

// fakeTray は systray の代わり。run は callbacks を記録し、release が閉じられるまで戻らない
// （本物の systray.Run がイベントループの終了まで戻らないのと同じ）。
type fakeTray struct {
	icons    [][]byte
	tooltips []string
	items    []string
	clicked  chan struct{}
	quitCh   chan struct{}
	started  chan struct{}
	release  chan struct{}
	onReady  func()
	onExit   func()
}

func newFakeTray() *fakeTray {
	return &fakeTray{
		clicked: make(chan struct{}, 1),
		quitCh:  make(chan struct{}, 10),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (f *fakeTray) api() trayAPI {
	return trayAPI{
		run: func(onReady, onExit func()) {
			f.onReady, f.onExit = onReady, onExit
			close(f.started)
			<-f.release
		},
		setIcon:    func(b []byte) { f.icons = append(f.icons, b) },
		setTooltip: func(s string) { f.tooltips = append(f.tooltips, s) },
		addItem: func(title, tooltip string) <-chan struct{} {
			f.items = append(f.items, title)
			return f.clicked
		},
		quit: func() { f.quitCh <- struct{}{} },
	}
}

// startRun は run を別の goroutine で動かし、fake の run に入るまで待つ。
// 戻り値の channel は run が戻ったときに閉じられる。
func startRun(t *testing.T, api trayAPI, started <-chan struct{}, opts Options) <-chan struct{} {
	t.Helper()
	returned := make(chan struct{})
	go func() {
		run(api, opts)
		close(returned)
	}()
	if started != nil {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("run did not call systray.Run")
		}
	}
	return returned
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestRunWiresIconTooltipMenuAndCallbacks(t *testing.T) {
	f := newFakeTray()
	readyCalls, quitCalls := 0, 0
	returned := startRun(t, f.api(), f.started, Options{
		Tooltip: "動作中: アイドル 60s / 移動量 10px",
		OnReady: func() { readyCalls++ },
		OnQuit:  func() { quitCalls++ },
	})
	if f.onReady == nil || f.onExit == nil {
		t.Fatal("run did not pass callbacks to systray.Run")
	}

	f.onReady()
	if len(f.icons) != 1 || !bytes.Equal(f.icons[0], iconBytes) || len(iconBytes) == 0 {
		t.Errorf("SetIcon calls = %d, want 1 with embedded icon", len(f.icons))
	}
	if len(f.tooltips) != 1 || f.tooltips[0] != "動作中: アイドル 60s / 移動量 10px" {
		t.Errorf("tooltips = %q", f.tooltips)
	}
	if len(f.items) != 1 || f.items[0] != "終了" {
		t.Errorf("menu items = %q, want exactly [終了]", f.items)
	}
	if readyCalls != 1 {
		t.Errorf("OnReady calls = %d, want 1", readyCalls)
	}
	select {
	case <-f.quitCh:
		t.Fatal("quit called before click")
	case <-time.After(20 * time.Millisecond):
	}

	f.clicked <- struct{}{}
	waitClosed(t, f.quitCh, "quit after clicking 終了")

	if quitCalls != 0 {
		t.Errorf("OnQuit called before onExit")
	}
	f.onExit()
	if quitCalls != 1 {
		t.Errorf("OnQuit calls = %d, want 1", quitCalls)
	}
	close(f.release)
	waitClosed(t, returned, "run to return")
	if quitCalls != 1 {
		t.Errorf("OnQuit calls after run returned = %d, want 1", quitCalls)
	}
}

func TestRunWithNilCallbacksIsSafe(t *testing.T) {
	f := newFakeTray()
	returned := startRun(t, f.api(), f.started, Options{Tooltip: "x"})
	f.onReady()
	f.onExit()
	if len(f.items) != 1 {
		t.Errorf("menu items = %q, want 1", f.items)
	}
	close(f.release)
	waitClosed(t, returned, "run to return with nil OnQuit")
}

// Windows の systray.Quit は、onExit をクリックした goroutine で呼び、イベントループは
// onExit の終了を待たずに終わる。それでも Run は OnQuit が終わるまで戻ってはいけない。
func TestRunWaitsForOnQuitStartedOnAnotherGoroutine(t *testing.T) {
	entered := make(chan struct{})
	unblock := make(chan struct{})
	var calls atomic.Int32
	api := newFakeTray().api()
	api.run = func(onReady, onExit func()) {
		go onExit()
		<-entered // onExit が OnQuit に入ってからイベントループが終わる
	}
	returned := startRun(t, api, nil, Options{OnQuit: func() {
		calls.Add(1)
		close(entered)
		<-unblock
	}})

	waitClosed(t, entered, "OnQuit to start")
	select {
	case <-returned:
		t.Fatal("run returned while OnQuit was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(unblock)
	waitClosed(t, returned, "run to return after OnQuit finished")
	if n := calls.Load(); n != 1 {
		t.Errorf("OnQuit calls = %d, want 1", n)
	}
}

func TestOnQuitRunsOnlyOnceWhenOnExitCalledTwice(t *testing.T) {
	var calls atomic.Int32
	api := newFakeTray().api()
	api.run = func(onReady, onExit func()) {
		onExit()
		onExit()
	}
	returned := startRun(t, api, nil, Options{OnQuit: func() { calls.Add(1) }})
	waitClosed(t, returned, "run to return")
	if n := calls.Load(); n != 1 {
		t.Errorf("OnQuit calls = %d, want 1", n)
	}
}

// イベントループが onExit を呼ばずに終わった（メッセージループの失敗など）ときも、
// Run は OnQuit を 1 回呼んでから戻る。後片付けを飛ばさないため。
func TestRunCallsOnQuitWhenLoopEndsWithoutOnExit(t *testing.T) {
	var calls atomic.Int32
	api := newFakeTray().api()
	api.run = func(onReady, onExit func()) {}
	returned := startRun(t, api, nil, Options{OnQuit: func() { calls.Add(1) }})
	waitClosed(t, returned, "run to return")
	if n := calls.Load(); n != 1 {
		t.Errorf("OnQuit calls = %d, want 1", n)
	}
}
