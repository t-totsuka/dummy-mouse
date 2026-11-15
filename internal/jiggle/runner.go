package jiggle

import (
	"context"
	"time"
)

// Runner はアイドル判定のループを回し、アイドル時間に達したらカーソルを 1 周させる。
//
// Runner は 1 つの goroutine で動く。外部と共有するのは Tracker だけ。
type Runner struct {
	s Settings
	m Mouse
	c Clock
	t *Tracker

	lastSeen    Point     // 最後に確認したカーソル位置
	hasLastSeen bool      // lastSeen が有効か（起動時に位置を取れなかった場合は false）
	lastLapEnd  time.Time // 最後の 1 周を終えた時刻（ゼロ値なら判定に使わない）
}

// NewRunner は設定 s で動く Runner を返す。
func NewRunner(s Settings, m Mouse, c Clock, t *Tracker) *Runner {
	return &Runner{s: s, m: m, c: c, t: t}
}

// Run は ctx が cancel されるまで処理を続け、ctx.Err() を返す。
// Mouse の error では終了しない（2.5）。
func (r *Runner) Run(ctx context.Context) error {
	// 起動したときの位置を既知の位置にする（起動時の位置を操作と見なさない）。
	if p, err := r.m.CursorPos(); err == nil {
		r.lastSeen, r.hasLastSeen = p, true
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.c.After(r.s.PollInterval):
		}
		if err := r.tick(ctx); err != nil {
			return err
		}
	}
}

// tick は PollInterval ごとの判定を 1 回行う。ctx が cancel されたときだけ error を返す。
func (r *Runner) tick(ctx context.Context) error {
	now := r.c.Now()
	pos, err := r.m.CursorPos()
	if err != nil {
		return nil // 位置を取れない tick は判定を飛ばす
	}
	if !r.hasLastSeen {
		r.lastSeen, r.hasLastSeen = pos, true
	} else if pos != r.lastSeen {
		// 予想外の位置の変化はユーザーの操作と見なす（フックの取りこぼしを補う）。
		r.t.MarkActivity(now)
		r.lastSeen = pos
	}

	// 基準時刻 = max(最後の操作, 最後の 1 周の終了)。
	base := r.t.LastActivity()
	if r.lastLapEnd.After(base) {
		base = r.lastLapEnd
	}
	if now.Sub(base) < r.s.IdleThreshold {
		return nil
	}
	return r.runLap(ctx, now, pos)
}

// runLap は lapStart の時刻に start から 1 周させる。終わるか打ち切ったら lastLapEnd と lastSeen を更新する。
// ctx が cancel されたときだけ error を返す。
func (r *Runner) runLap(ctx context.Context, lapStart time.Time, start Point) error {
	defer r.finishLap()

	bounds, err := r.m.Bounds()
	if err != nil {
		return nil // 1 周を打ち切り、次の周期に再び試す
	}
	targets := LapTargets(start, r.s.StepPixels, bounds)
	for i, target := range targets {
		if i > 0 {
			if err := r.wait(ctx, r.s.StepDelay); err != nil {
				return err
			}
			if r.userInterrupted(lapStart, targets[i-1]) {
				return nil // 残りの移動を取りやめ、カーソルはユーザーが置いた位置のままにする（2.4）
			}
		}
		if err := r.m.MoveTo(target); err != nil {
			return nil // 1 周を打ち切り、次の周期に再び試す
		}
	}
	return nil
}

// userInterrupted は 1 周の途中でユーザーが操作したかを判定する（2.4）。
// (a) 1 周を始めたあとに操作が記録された、(b) カーソルが直前の目標座標からずれている、のどちらかで true。
// (b) のときは、その変化を操作として記録する。位置を取れないときは (a) だけで判定する。
func (r *Runner) userInterrupted(lapStart time.Time, prevTarget Point) bool {
	if r.t.LastActivity().After(lapStart) {
		return true
	}
	if p, err := r.m.CursorPos(); err == nil && p != prevTarget {
		r.t.MarkActivity(r.c.Now())
		return true
	}
	return false
}

// finishLap は 1 周の終了（または打ち切り）を記録する。
// 自分が動かした結果の位置を既知の位置にして、次の tick で操作と見なさない（1.4）。
func (r *Runner) finishLap() {
	r.lastLapEnd = r.c.Now()
	if p, err := r.m.CursorPos(); err == nil {
		r.lastSeen, r.hasLastSeen = p, true
	}
}

// wait は d だけ待つ。待つ間に ctx が cancel されたら ctx.Err() を返す。
func (r *Runner) wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.c.After(d):
		return nil
	}
}
