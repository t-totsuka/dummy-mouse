package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mousemover/internal/config"
	"mousemover/internal/jiggle"
	"mousemover/internal/tray"
)

// このテストは本物の Win32 / トレイを使わない。すべての依存を偽物に差し替える。

type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *fixedClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

type stubMouse struct{}

func (stubMouse) CursorPos() (jiggle.Point, error)            { return jiggle.Point{}, nil }
func (stubMouse) MoveTo(jiggle.Point) error                   { return nil }
func (stubMouse) Bounds() (jiggle.Rect, error)                { return jiggle.Rect{Right: 100, Bottom: 100}, nil }
func (stubMouse) IsOwnInput(uintptr, bool, jiggle.Point) bool { return false }

// recorder は呼び出しの順序を記録する。
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *recorder) has(e string) bool {
	for _, x := range r.list() {
		if x == e {
			return true
		}
	}
	return false
}

type fakeHook struct {
	rec   *recorder
	stops int
	mu    sync.Mutex
}

func (h *fakeHook) Stop() error {
	h.mu.Lock()
	h.stops++
	h.mu.Unlock()
	h.rec.add("hook.Stop")
	return errors.New("stop error is ignored")
}

type fakeRunner struct {
	rec     *recorder
	ctxSeen chan context.Context
}

func (r *fakeRunner) Run(ctx context.Context) error {
	r.rec.add("runner.Run")
	r.ctxSeen <- ctx
	<-ctx.Done()
	r.rec.add("runner.returned")
	return ctx.Err()
}

type harness struct {
	rec        *recorder
	clock      *fixedClock
	hook       *fakeHook
	runner     *fakeRunner
	errors     []string
	onActivity func()
	settings   jiggle.Settings
	tracker    *jiggle.Tracker
	trayOpts   *tray.Options
	hookErr    error
	dpiErr     error
	trayFn     func(opts tray.Options) // nil なら OnReady → OnQuit の順に呼ぶ
}

func newHarness() *harness {
	rec := &recorder{}
	return &harness{
		rec:    rec,
		clock:  &fixedClock{now: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)},
		hook:   &fakeHook{rec: rec},
		runner: &fakeRunner{rec: rec, ctxSeen: make(chan context.Context, 1)},
	}
}

func (h *harness) deps() deps {
	return deps{
		parseConfig: config.Parse,
		showError: func(m string) {
			h.rec.add("showError")
			h.errors = append(h.errors, m)
		},
		enableDPI: func() error {
			h.rec.add("enableDPI")
			return h.dpiErr
		},
		newMouse: func() appMouse { return stubMouse{} },
		startHook: func(onActivity func(), m appMouse) (stopper, error) {
			h.rec.add("startHook")
			if h.hookErr != nil {
				return nil, h.hookErr
			}
			h.onActivity = onActivity
			return h.hook, nil
		},
		newRunner: func(s jiggle.Settings, m jiggle.Mouse, c jiggle.Clock, t *jiggle.Tracker) loopRunner {
			h.settings, h.tracker = s, t
			return h.runner
		},
		runTray: func(opts tray.Options) {
			h.rec.add("runTray")
			h.trayOpts = &opts
			if h.trayFn != nil {
				h.trayFn(opts)
				return
			}
			opts.OnReady()
			<-h.runner.ctxSeen // Runner が起動したことを確かめてから終了を選ぶ
			opts.OnQuit()
		},
		clock: h.clock,
	}
}

func TestRunInvalidArgsShowsErrorAndExits2(t *testing.T) {
	cases := [][]string{
		{"-idle", "0"},
		{"-idle", "abc"},
		{"-step", "-5"},
		{"-foo"},
		{"extra"},
		{"-h"},
		{"-help"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness()
			_, wantErr := config.Parse(args)
			if wantErr == nil {
				t.Fatalf("前提: config.Parse(%v) はエラーになるはず", args)
			}
			if got := run(args, h.deps()); got != 2 {
				t.Fatalf("run = %d, want 2", got)
			}
			if len(h.errors) != 1 || h.errors[0] != wantErr.Error() {
				t.Fatalf("showError = %q, want [%q]", h.errors, wantErr.Error())
			}
			for _, e := range []string{"enableDPI", "startHook", "runTray"} {
				if h.rec.has(e) {
					t.Errorf("%s が呼ばれた（events=%v）", e, h.rec.list())
				}
			}
		})
	}
}

func TestRunHookFailureShowsErrorAndExits1(t *testing.T) {
	h := newHarness()
	h.hookErr = errors.New("SetWindowsHookExW failed")
	if got := run(nil, h.deps()); got != 1 {
		t.Fatalf("run = %d, want 1", got)
	}
	if len(h.errors) != 1 || !strings.Contains(h.errors[0], "SetWindowsHookExW failed") {
		t.Fatalf("showError = %q, want a message containing the hook error", h.errors)
	}
	if h.rec.has("runTray") || h.rec.has("runner.Run") {
		t.Fatalf("トレイまたは Runner が起動した（events=%v）", h.rec.list())
	}
}

func TestRunNormalPathStartsRunnerAndStopsInOrder(t *testing.T) {
	h := newHarness()
	if got := run([]string{"-idle", "90s", "-step", "3"}, h.deps()); got != 0 {
		t.Fatalf("run = %d, want 0", got)
	}
	want := []string{"enableDPI", "startHook", "runTray", "runner.Run", "runner.returned", "hook.Stop"}
	if got := h.rec.list(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if h.hook.stops != 1 {
		t.Fatalf("hook.Stop の回数 = %d, want 1", h.hook.stops)
	}
	wantSettings := jiggle.Settings{
		IdleThreshold: 90 * time.Second,
		StepPixels:    3,
		PollInterval:  jiggle.DefaultPollInterval,
		StepDelay:     jiggle.DefaultStepDelay,
	}
	if h.settings != wantSettings {
		t.Fatalf("settings = %+v, want %+v", h.settings, wantSettings)
	}
	cfg, _ := config.Parse([]string{"-idle", "90s", "-step", "3"})
	if h.trayOpts == nil || h.trayOpts.Tooltip != cfg.Summary() {
		t.Fatalf("tooltip = %+v, want %q", h.trayOpts, cfg.Summary())
	}
}

func TestRunContinuesWhenDPIFails(t *testing.T) {
	h := newHarness()
	h.dpiErr = errors.New("access denied")
	if got := run(nil, h.deps()); got != 0 {
		t.Fatalf("run = %d, want 0", got)
	}
	if len(h.errors) != 0 {
		t.Fatalf("showError = %q, want none", h.errors)
	}
	if !h.rec.has("startHook") || !h.rec.has("runner.Run") {
		t.Fatalf("DPI の失敗のあと続行していない（events=%v）", h.rec.list())
	}
}

func TestRunOnQuitWithoutOnReadyReturnsPromptly(t *testing.T) {
	h := newHarness()
	h.trayFn = func(opts tray.Options) { opts.OnQuit() } // イベントループの異常終了
	done := make(chan int, 1)
	go func() { done <- run(nil, h.deps()) }()
	select {
	case got := <-done:
		if got != 0 {
			t.Fatalf("run = %d, want 0", got)
		}
	case <-time.After(time.Second):
		t.Fatal("OnReady なしの OnQuit で run が戻らない")
	}
	if h.rec.has("runner.Run") {
		t.Fatalf("Runner が起動した（events=%v）", h.rec.list())
	}
	if h.hook.stops != 1 {
		t.Fatalf("hook.Stop の回数 = %d, want 1", h.hook.stops)
	}
}

func TestRunOnQuitIsIdempotentAndOnReadyAfterQuitDoesNothing(t *testing.T) {
	h := newHarness()
	h.trayFn = func(opts tray.Options) {
		opts.OnQuit()
		opts.OnQuit()
		opts.OnReady()
	}
	if got := run(nil, h.deps()); got != 0 {
		t.Fatalf("run = %d, want 0", got)
	}
	if h.hook.stops != 1 {
		t.Fatalf("hook.Stop の回数 = %d, want 1", h.hook.stops)
	}
	if h.rec.has("runner.Run") {
		t.Fatalf("quit のあとに Runner が起動した（events=%v）", h.rec.list())
	}
}

func TestRunHookActivityMarksTracker(t *testing.T) {
	h := newHarness()
	later := h.clock.Now().Add(3 * time.Minute)
	h.trayFn = func(opts tray.Options) {
		h.clock.set(later)
		h.onActivity()
	}
	if got := run(nil, h.deps()); got != 0 {
		t.Fatalf("run = %d, want 0", got)
	}
	if h.tracker == nil {
		t.Fatal("Tracker が Runner に渡されていない")
	}
	if !h.tracker.LastActivity().Equal(later) {
		t.Fatalf("LastActivity = %v, want %v", h.tracker.LastActivity(), later)
	}
}

func TestRealDepsAreWired(t *testing.T) {
	// 本物の依存は呼ばずに、すべて設定されていることだけ確かめる。
	d := realDeps()
	if d.parseConfig == nil || d.showError == nil || d.enableDPI == nil || d.newMouse == nil ||
		d.startHook == nil || d.newRunner == nil || d.runTray == nil || d.clock == nil {
		t.Fatalf("realDeps に未設定の依存がある: %+v", d)
	}
	if _, ok := d.newRunner(jiggle.Settings{}, stubMouse{}, jiggle.SystemClock{}, jiggle.NewTracker(time.Now())).(*jiggle.Runner); !ok {
		t.Fatal("newRunner が *jiggle.Runner を返さない")
	}
}
