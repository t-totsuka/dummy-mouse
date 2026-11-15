// Package tray は、タスクトレイのアイコン、ツールチップ、メニューを扱う。
package tray

import (
	_ "embed"
	"sync"

	"fyne.io/systray"
)

//go:embed icon.ico
var iconBytes []byte

const (
	quitTitle   = "終了"
	quitTooltip = "Idle Mouse Mover を終了します"
)

// Options は Run の設定。
type Options struct {
	Tooltip string // Config.Summary()
	OnReady func() // トレイの準備ができたら呼ばれる。ここで Runner を起動する
	OnQuit  func() // 「終了」を選んだあと、Run が戻る前に 1 回だけ呼ばれる
}

// trayAPI は systray の呼び出しを差し替えられるようにするための継ぎ目。
type trayAPI struct {
	run        func(onReady, onExit func())
	setIcon    func(icon []byte)
	setTooltip func(tooltip string)
	addItem    func(title, tooltip string) <-chan struct{}
	quit       func()
}

var systrayAPI = trayAPI{
	run:        systray.Run,
	setIcon:    systray.SetIcon,
	setTooltip: systray.SetTooltip,
	addItem: func(title, tooltip string) <-chan struct{} {
		// AddMenuItem の項目はチェックできない状態・未チェックで作られる。
		return systray.AddMenuItem(title, tooltip).ClickedCh
	},
	quit: systray.Quit,
}

// Run は「終了」が選ばれるまで処理を止めて待つ。アイコンは go:embed した icon.ico を使う。
// Run は OnQuit が終わってから戻る。Windows の systray.Quit は onExit をクリックした
// goroutine で呼び、イベントループは onExit の終了を待たずに終わるため、Run 側で待ち合わせる。
// イベントループが onExit を呼ばずに終わったときも、Run は OnQuit を 1 回呼んでから戻る。
func Run(opts Options) {
	run(systrayAPI, opts)
}

func run(api trayAPI, opts Options) {
	onReady, onExit := callbacks(api, opts)
	api.run(onReady, onExit)
	// onExit がほかの goroutine で実行中なら、sync.Once により終わるまでここで待つ。
	// まだ呼ばれていなければ、ここで OnQuit を呼ぶ。
	onExit()
}

// callbacks は systray.Run に渡す onReady / onExit を作る。
// onExit は何回、どの goroutine から呼ばれても OnQuit を 1 回だけ呼び、
// 実行中に呼ばれた場合は OnQuit が終わるまで戻らない。
func callbacks(api trayAPI, opts Options) (onReady, onExit func()) {
	onReady = func() {
		api.setIcon(iconBytes)
		api.setTooltip(opts.Tooltip)
		clicked := api.addItem(quitTitle, quitTooltip)
		go func() {
			<-clicked
			api.quit()
		}()
		if opts.OnReady != nil {
			opts.OnReady()
		}
	}
	var quitOnce sync.Once
	onExit = func() {
		quitOnce.Do(func() {
			if opts.OnQuit != nil {
				opts.OnQuit()
			}
		})
	}
	return onReady, onExit
}
