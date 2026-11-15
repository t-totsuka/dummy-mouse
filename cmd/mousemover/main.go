// Command mousemover は、マウス操作が一定時間ないときにカーソルを小さく 1 周させるタスクトレイ常駐アプリである。
package main

import (
	"context"
	"os"
	"sync"
	"time"

	"mousemover/internal/config"
	"mousemover/internal/jiggle"
	"mousemover/internal/platform"
	"mousemover/internal/tray"
)

// 終了コード（design.md「Error Handling」）。
const (
	exitOK          = 0 // 正常終了（「終了」を選んだ）
	exitInitFailure = 1 // 初期化の失敗（フックを登録できない）
	exitConfigError = 2 // 起動オプションの誤り（-h / -help を含む）
)

// dialogTitle はエラーダイアログのタイトル。
const dialogTitle = "Idle Mouse Mover"

// runnerStopTimeout は、OnQuit で Runner.Run が戻るのを待つ上限。
// Runner は ctx の cancel ですぐに戻るため、通常はこの時間に達しない。
var runnerStopTimeout = 5 * time.Second

// appMouse は、Runner が使う jiggle.Mouse と、フックが使う自分の入力の見分けを兼ねる。
// platform.Mouse がこれを満たす。
type appMouse interface {
	jiggle.Mouse
	IsOwnInput(extraInfo uintptr, injected bool, pt jiggle.Point) bool
}

// stopper は開始したフックの停止。
type stopper interface{ Stop() error }

// loopRunner はアイドル判定のループ。jiggle.Runner がこれを満たす。
type loopRunner interface {
	Run(ctx context.Context) error
}

// deps は run が使う外部の依存。テストでは偽物に差し替える。
type deps struct {
	parseConfig func(args []string) (config.Config, error)
	showError   func(message string)
	enableDPI   func() error
	newMouse    func() appMouse
	startHook   func(onActivity func(), m appMouse) (stopper, error)
	newRunner   func(s jiggle.Settings, m jiggle.Mouse, c jiggle.Clock, t *jiggle.Tracker) loopRunner
	runTray     func(opts tray.Options)
	clock       jiggle.Clock
}

func main() {
	os.Exit(run(os.Args[1:], realDeps()))
}

// realDeps は本物の Windows の実装を使う依存を返す。
func realDeps() deps {
	return deps{
		parseConfig: config.Parse,
		showError:   func(message string) { platform.ShowError(dialogTitle, message) },
		enableDPI:   platform.EnableDPIAwareness,
		newMouse:    func() appMouse { return platform.NewMouse() },
		startHook: func(onActivity func(), m appMouse) (stopper, error) {
			h, err := platform.StartHook(onActivity, m)
			if err != nil {
				return nil, err // nil の *Hook を interface に入れないため
			}
			return h, nil
		},
		newRunner: func(s jiggle.Settings, m jiggle.Mouse, c jiggle.Clock, t *jiggle.Tracker) loopRunner {
			return jiggle.NewRunner(s, m, c, t)
		},
		runTray: tray.Run,
		clock:   jiggle.SystemClock{},
	}
}

// run は「起動と終了」のシーケンス（design.md）のとおりに全体を組み立て、終了コードを返す。
func run(args []string, d deps) int {
	cfg, err := d.parseConfig(args)
	if err != nil {
		// 要件 4.4: 理由を表示し、常駐を始めずに終了する。
		d.showError(err.Error())
		return exitConfigError
	}

	// すでに設定済みの場合などがあるため、失敗しても続行する。
	_ = d.enableDPI()

	tracker := jiggle.NewTracker(d.clock.Now())
	mouse := d.newMouse()
	// onActivity はフックのスレッドから呼ばれるので、すぐに戻る処理だけにする。
	hook, err := d.startHook(func() { tracker.MarkActivity(d.clock.Now()) }, mouse)
	if err != nil {
		// 要件 1.1 をフックなしでは満たせないため、早めに失敗させる。
		d.showError("マウスの操作を検知する準備ができませんでした。\n" + err.Error())
		return exitInitFailure
	}

	runner := d.newRunner(jiggle.Settings{
		IdleThreshold: cfg.IdleThreshold,
		StepPixels:    cfg.StepPixels,
		PollInterval:  jiggle.DefaultPollInterval,
		StepDelay:     jiggle.DefaultStepDelay,
	}, mouse, d.clock, tracker)

	lc := newLifecycle(runner, hook)
	d.runTray(tray.Options{
		Tooltip: cfg.Summary(),
		OnReady: lc.start,
		OnQuit:  lc.quit,
	})
	// tray.Run は OnQuit が終わってから戻る。念のためここでも停止を済ませる（2 回目以降は何もしない）。
	lc.quit()
	return exitOK
}

// lifecycle は Runner の起動と、Runner・フックの停止を受け持つ。
type lifecycle struct {
	runner loopRunner
	hook   stopper

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	started  bool
	quitting bool
	done     chan struct{} // Runner.Run が戻ったら close される

	quitOnce sync.Once
}

func newLifecycle(r loopRunner, h stopper) *lifecycle {
	ctx, cancel := context.WithCancel(context.Background())
	return &lifecycle{runner: r, hook: h, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

// start はトレイの準備ができたら呼ばれ、Runner を goroutine で起動する。
// 2 回目以降と、quit のあとの呼び出しでは何もしない。
func (l *lifecycle) start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started || l.quitting {
		return
	}
	l.started = true
	go func() {
		defer close(l.done)
		_ = l.runner.Run(l.ctx) // ctx の cancel でのみ戻る
	}()
}

// quit は ctx を cancel し、Runner が起動していれば戻るのを待ってから、フックを止める。
// 何回呼ばれても処理は 1 回だけ。OnReady が呼ばれていなくても待ち続けない。
func (l *lifecycle) quit() {
	l.quitOnce.Do(func() {
		l.mu.Lock()
		l.quitting = true
		started := l.started
		l.mu.Unlock()

		l.cancel()
		if started {
			select {
			case <-l.done:
			case <-time.After(runnerStopTimeout):
			}
		}
		// 停止のエラーは表示しない（ログは出さない。スレッドの終了で OS がフックを外す）。
		_ = l.hook.Stop()
	})
}
