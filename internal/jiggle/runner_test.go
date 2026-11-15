package jiggle

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

var (
	runnerStart  = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	runnerBounds = Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	runnerOrigin = Point{X: 100, Y: 100}
)

func runnerSettings() Settings {
	return Settings{
		IdleThreshold: 5 * time.Minute,
		StepPixels:    10,
		PollInterval:  time.Second,
		StepDelay:     50 * time.Millisecond,
	}
}

// moveRecord は MoveTo が呼ばれたときの偽の時刻と目標座標。
type moveRecord struct {
	at time.Time
	p  Point
}

// runnerHarness は偽の時計・偽のマウス・Tracker と、別 goroutine で動く Runner をまとめたもの。
type runnerHarness struct {
	clk     *fakeClock
	mouse   *fakeMouse
	tracker *Tracker
	cancel  context.CancelFunc
	done    chan error

	mu    sync.Mutex
	moves []moveRecord
}

// startRunner は Runner を起動し、最初のポーリングのタイマーが登録されるまで待つ。
// テストの終わりに ctx を cancel し、Run が戻るのを待つ（goroutine を残さない）。
func startRunner(t *testing.T, start Point) *runnerHarness {
	t.Helper()
	h := &runnerHarness{
		clk:     newFakeClock(runnerStart),
		mouse:   newFakeMouse(start, runnerBounds),
		tracker: NewTracker(runnerStart),
		done:    make(chan error, 1),
	}
	h.mouse.SetOnMoveTo(func(p Point) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.moves = append(h.moves, moveRecord{at: h.clk.Now(), p: p})
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	r := NewRunner(runnerSettings(), h.mouse, h.clk, h.tracker)
	go func() { h.done <- r.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(2 * time.Second):
			t.Errorf("Run が ctx の cancel のあと戻りませんでした")
		}
	})
	waitPending(t, h.clk)
	return h
}

// waitPending は Runner の goroutine が処理を終えて次のタイマーを登録するまで待つ。
// BlockUntil(t, 1) と同じ意味だが、スリープせずに Gosched で回すので速い。
func waitPending(t *testing.T, c *fakeClock) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for c.Pending() < 1 {
		if time.Now().After(deadline) {
			c.BlockUntil(t, 1) // 失敗のメッセージを出させる
			return
		}
		runtime.Gosched()
	}
}

// earliestDeadline は未発火のタイマーのうち最も早い期限を返す。
func earliestDeadline(c *fakeClock) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var earliest time.Time
	for i, tm := range c.timers {
		if i == 0 || tm.deadline.Before(earliest) {
			earliest = tm.deadline
		}
	}
	return earliest, len(c.timers) > 0
}

// advanceTo は偽の時刻を、起動時刻から offset の時点まで 1 段ずつ進める。
// 各段では BlockUntil 相当で待ってから、「残り」と「次のタイマーの期限まで」の短い方だけ Advance する。
// こうすると各タイマーは自分の期限ちょうどの時刻で発火する（PollInterval / StepDelay 以下の幅で進める）。
// 最後に Runner が次のタイマーを登録するまで待つので、戻ったあとは Runner の状態が落ち着いている。
func (h *runnerHarness) advanceTo(t *testing.T, offset time.Duration) {
	t.Helper()
	target := runnerStart.Add(offset)
	const maxSteps = 100000 // ハングを防ぐ上限
	for i := 0; ; i++ {
		if i > maxSteps {
			t.Fatalf("advanceTo(%v): %d 段を超えても到達しませんでした", offset, maxSteps)
		}
		waitPending(t, h.clk)
		now := h.clk.Now()
		if !now.Before(target) {
			return
		}
		d := target.Sub(now)
		if dl, ok := earliestDeadline(h.clk); ok && dl.Sub(now) < d {
			d = dl.Sub(now)
		}
		h.clk.Advance(d)
	}
}

func (h *runnerHarness) recorded() []moveRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]moveRecord(nil), h.moves...)
}

// laps は記録された MoveTo を 4 回ずつの 1 周に分ける。
func laps(t *testing.T, moves []moveRecord) [][]moveRecord {
	t.Helper()
	if len(moves)%4 != 0 {
		t.Fatalf("MoveTo の回数が 4 の倍数ではありません: %d 回 %v", len(moves), moves)
	}
	var out [][]moveRecord
	for i := 0; i < len(moves); i += 4 {
		out = append(out, moves[i:i+4])
	}
	return out
}

// assertLapStart は 1 周の開始が [起動時刻+from, 起動時刻+from+within] に入っていることを確かめる。
func assertLapStart(t *testing.T, name string, lap []moveRecord, from, within time.Duration) {
	t.Helper()
	got := lap[0].at.Sub(runnerStart)
	if got < from || got > from+within {
		t.Errorf("%s の開始 = %v, want %v〜%v", name, got, from, from+within)
	}
}

func assertTargets(t *testing.T, lap []moveRecord, start Point) {
	t.Helper()
	want := LapTargets(start, 10, runnerBounds)
	for i, m := range lap {
		if m.p != want[i] {
			t.Errorf("%d 歩目の目標 = %v, want %v", i+1, m.p, want[i])
		}
	}
}

// (1) 4 分 59 秒までは動かず、5 分で上 → 右上 → 右 → 開始位置の 4 回移動する（1.3, 2.1）。
func TestRunnerFirstLapAtIdleThreshold(t *testing.T) {
	h := startRunner(t, runnerOrigin)

	h.advanceTo(t, 4*time.Minute+59*time.Second)
	if n := len(h.recorded()); n != 0 {
		t.Fatalf("4 分 59 秒までに MoveTo が %d 回呼ばれました, want 0", n)
	}

	h.advanceTo(t, 5*time.Minute+time.Second)
	moves := h.recorded()
	if len(moves) != 4 {
		t.Fatalf("5 分のあとの MoveTo = %d 回, want 4: %v", len(moves), moves)
	}
	want := []Point{{100, 90}, {110, 90}, {110, 100}, {100, 100}}
	for i, m := range moves {
		if m.p != want[i] {
			t.Errorf("%d 歩目の目標 = %v, want %v", i+1, m.p, want[i])
		}
	}
	assertLapStart(t, "1 周目", moves, 5*time.Minute, 0)
	// 各ステップの間は StepDelay だけ空ける。
	for i := 1; i < len(moves); i++ {
		if d := moves[i].at.Sub(moves[i-1].at); d != 50*time.Millisecond {
			t.Errorf("%d 歩目と %d 歩目の間隔 = %v, want 50ms", i, i+1, d)
		}
	}
}

// (2) 1 周のあと、カーソルは開始位置にある（2.2）。
func TestRunnerLapEndsAtStart(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, 5*time.Minute+time.Second)
	if n := len(h.recorded()); n != 4 {
		t.Fatalf("MoveTo = %d 回, want 4", n)
	}
	if got := h.mouse.Pos(); got != runnerOrigin {
		t.Errorf("1 周のあとの位置 = %v, want %v", got, runnerOrigin)
	}
}

// (3) 操作がないままなら、10 分、15 分でも 1 周する（2.3）。
func TestRunnerRepeatsLapsWhileIdle(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, 15*time.Minute+2*time.Second)

	ls := laps(t, h.recorded())
	if len(ls) != 3 {
		t.Fatalf("15 分までの 1 周の回数 = %d, want 3", len(ls))
	}
	assertLapStart(t, "1 周目", ls[0], 5*time.Minute, 2*time.Second)
	assertLapStart(t, "2 周目", ls[1], 10*time.Minute, 2*time.Second)
	assertLapStart(t, "3 周目", ls[2], 15*time.Minute, 2*time.Second)
	for _, l := range ls {
		assertTargets(t, l, runnerOrigin)
	}
	if got := h.mouse.Pos(); got != runnerOrigin {
		t.Errorf("3 周のあとの位置 = %v, want %v", got, runnerOrigin)
	}
}

// (4) 4 分の時点で MarkActivity を呼ぶと、次の 1 周は 5 分ではなく 9 分になる（1.2）。
func TestRunnerActivityResetsIdleTimer(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, 4*time.Minute)
	h.tracker.MarkActivity(h.clk.Now())

	h.advanceTo(t, 8*time.Minute+59*time.Second)
	if n := len(h.recorded()); n != 0 {
		t.Fatalf("8 分 59 秒までに MoveTo が %d 回呼ばれました, want 0", n)
	}

	h.advanceTo(t, 9*time.Minute+2*time.Second)
	ls := laps(t, h.recorded())
	if len(ls) != 1 {
		t.Fatalf("9 分までの 1 周の回数 = %d, want 1", len(ls))
	}
	assertLapStart(t, "1 周目", ls[0], 9*time.Minute, 2*time.Second)
}

// (5) 外部から位置を変えると、ポーリングで操作と見なされる（1.1 の補助経路）。
func TestRunnerExternalMoveCountsAsActivity(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, 3*time.Minute)
	moved := Point{X: 500, Y: 400}
	h.mouse.SetPos(moved) // ユーザーがカーソルを動かした

	h.advanceTo(t, 7*time.Minute+59*time.Second)
	if n := len(h.recorded()); n != 0 {
		t.Fatalf("7 分 59 秒までに MoveTo が %d 回呼ばれました, want 0", n)
	}
	if got := h.tracker.LastActivity().Sub(runnerStart); got < 3*time.Minute || got > 3*time.Minute+time.Second {
		t.Errorf("記録された操作の時刻 = 起動から %v, want 3m〜3m1s", got)
	}

	h.advanceTo(t, 8*time.Minute+2*time.Second)
	ls := laps(t, h.recorded())
	if len(ls) != 1 {
		t.Fatalf("8 分までの 1 周の回数 = %d, want 1", len(ls))
	}
	assertLapStart(t, "1 周目", ls[0], 8*time.Minute, 2*time.Second)
	assertTargets(t, ls[0], moved) // ユーザーが動かした位置から 1 周する
	if got := h.mouse.Pos(); got != moved {
		t.Errorf("1 周のあとの位置 = %v, want %v", got, moved)
	}
}

// (6) 自分の 1 周による位置の変化は操作と見なさない。次の 1 周は 1 周目の終了からちょうど 5 分後（1.4）。
// 開始位置を画面の外にして、1 周の前後で位置が変わる（範囲内に収めた位置に戻る）ようにしている。
func TestRunnerOwnLapIsNotActivity(t *testing.T) {
	outside := Point{X: -5, Y: 100}
	h := startRunner(t, outside)
	h.advanceTo(t, 10*time.Minute+2*time.Second)

	ls := laps(t, h.recorded())
	if len(ls) != 2 {
		t.Fatalf("10 分までの 1 周の回数 = %d, want 2", len(ls))
	}
	clamped := Point{X: 0, Y: 100}
	if got := h.mouse.Pos(); got != clamped {
		t.Fatalf("1 周のあとの位置 = %v, want %v", got, clamped)
	}
	if got := h.tracker.LastActivity(); !got.Equal(runnerStart) {
		t.Errorf("LastActivity = %v, want 起動時刻 %v（自分の移動が操作として記録された）", got, runnerStart)
	}
	lap1End := ls[0][3].at
	wantLap2 := lap1End.Add(5 * time.Minute)
	if got := ls[1][0].at; got.Before(wantLap2) || got.Sub(wantLap2) >= 500*time.Millisecond {
		t.Errorf("2 周目の開始 = 1 周目の終了から %v, want 5m（PollInterval 未満の誤差）", got.Sub(lap1End))
	}
}

// (7) ctx を cancel すると Run はすぐに context.Canceled を返す（3.3）。
func TestRunnerReturnsOnCancel(t *testing.T) {
	clk := newFakeClock(runnerStart)
	m := newFakeMouse(runnerOrigin, runnerBounds)
	r := NewRunner(runnerSettings(), m, clk, NewTracker(runnerStart))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	clk.BlockUntil(t, 1)

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run の戻り値 = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx の cancel のあと Run が戻りませんでした")
	}
}

// lapOffset は 1 周目の開始（起動から 5 分）からの経過時間を、起動からの経過時間にする。
func lapOffset(d time.Duration) time.Duration { return 5*time.Minute + d }

// (8) 1 歩目と 2 歩目の間に操作が記録されると、残りの移動を取りやめる。次の 1 周は操作から 5 分後（2.4）。
func TestRunnerAbortsLapOnActivity(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, lapOffset(20*time.Millisecond)) // 1 歩目のあと、StepDelay の途中
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("1 歩目のあとの MoveTo = %d 回, want 1", n)
	}
	activityAt := h.clk.Now()
	h.tracker.MarkActivity(activityAt) // ユーザーの操作（フックが記録した）

	h.advanceTo(t, lapOffset(5*time.Minute-time.Second))
	moves := h.recorded()
	if len(moves) != 1 {
		t.Fatalf("操作のあとの MoveTo = %d 回, want 1（残りを取りやめる）: %v", len(moves), moves)
	}
	if got, want := h.mouse.Pos(), (Point{100, 90}); got != want {
		t.Errorf("取りやめたあとの位置 = %v, want %v（カーソルを戻さない）", got, want)
	}

	h.advanceTo(t, lapOffset(5*time.Minute+2*time.Second))
	moves = h.recorded()[1:]
	ls := laps(t, moves)
	if len(ls) != 1 {
		t.Fatalf("次の 1 周の回数 = %d, want 1", len(ls))
	}
	if d := ls[0][0].at.Sub(activityAt); d < 5*time.Minute || d > 5*time.Minute+2*time.Second {
		t.Errorf("次の 1 周の開始 = 操作から %v, want 5m〜5m2s", d)
	}
	assertTargets(t, ls[0], Point{100, 90}) // 取りやめた位置から 1 周する
}

// (9) 1 歩目と 2 歩目の間にカーソルの位置が変わると、残りの移動を取りやめ、操作として記録する（2.4）。
func TestRunnerAbortsLapOnCursorMoved(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, lapOffset(0)) // 1 歩目の MoveTo のあと、StepDelay の待ちに入った
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("1 歩目のあとの MoveTo = %d 回, want 1", n)
	}
	user := Point{X: 800, Y: 600}
	h.mouse.SetPos(user) // ユーザーがカーソルを動かした（フックは取りこぼした）

	h.advanceTo(t, lapOffset(time.Second))
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("位置の変更のあとの MoveTo = %d 回, want 1（残りを取りやめる）", n)
	}
	if got := h.mouse.Pos(); got != user {
		t.Errorf("取りやめたあとの位置 = %v, want %v（ユーザーの位置のまま）", got, user)
	}
	activityAt := h.tracker.LastActivity()
	if d := activityAt.Sub(runnerStart.Add(lapOffset(0))); d < 0 || d > 50*time.Millisecond {
		t.Errorf("記録された操作の時刻 = 1 周の開始から %v, want 0〜50ms", d)
	}

	h.advanceTo(t, lapOffset(5*time.Minute-time.Second))
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("次の 1 周の前に MoveTo が %d 回呼ばれました, want 1", n)
	}
	h.advanceTo(t, lapOffset(5*time.Minute+2*time.Second))
	ls := laps(t, h.recorded()[1:])
	if len(ls) != 1 {
		t.Fatalf("次の 1 周の回数 = %d, want 1", len(ls))
	}
	if d := ls[0][0].at.Sub(activityAt); d < 5*time.Minute || d > 5*time.Minute+2*time.Second {
		t.Errorf("次の 1 周の開始 = 操作から %v, want 5m〜5m2s", d)
	}
	assertTargets(t, ls[0], user)
	if got := h.mouse.Pos(); got != user {
		t.Errorf("次の 1 周のあとの位置 = %v, want %v", got, user)
	}
}

// (10) 1 歩目の MoveTo が失敗しても Run は続き、5 分後に再び試して 4 回移動する（2.5）。
func TestRunnerRetriesAfterMoveToError(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.mouse.FailMoveTo(1, nil)

	h.advanceTo(t, lapOffset(5*time.Minute-time.Second))
	moves := h.recorded()
	if len(moves) != 1 {
		t.Fatalf("失敗した 1 周の MoveTo = %d 回, want 1（失敗で打ち切る）: %v", len(moves), moves)
	}
	failedAt := moves[0].at
	select {
	case err := <-h.done:
		t.Fatalf("MoveTo の失敗で Run が戻りました: %v", err)
	default:
	}
	if got := h.mouse.Pos(); got != runnerOrigin {
		t.Errorf("失敗のあとの位置 = %v, want %v", got, runnerOrigin)
	}

	h.advanceTo(t, lapOffset(5*time.Minute+2*time.Second))
	ls := laps(t, h.recorded()[1:])
	if len(ls) != 1 {
		t.Fatalf("再試行の 1 周の回数 = %d, want 1", len(ls))
	}
	if d := ls[0][0].at.Sub(failedAt); d < 5*time.Minute || d > 5*time.Minute+time.Second {
		t.Errorf("再試行の開始 = 失敗から %v, want 5m〜5m1s", d)
	}
	assertTargets(t, ls[0], runnerOrigin)
	if got := h.mouse.Pos(); got != runnerOrigin {
		t.Errorf("再試行のあとの位置 = %v, want %v", got, runnerOrigin)
	}
}

// (11) CursorPos が失敗した tick では移動せず、Run は続く。成功した tick で 1 周する（2.5）。
func TestRunnerSkipsTickOnCursorPosError(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, 4*time.Minute+58*time.Second)
	h.mouse.FailCursorPos(5, nil) // 4:59, 5:00, 5:01, 5:02, 5:03 の tick が失敗する

	h.advanceTo(t, 5*time.Minute+3*time.Second+500*time.Millisecond)
	if n := len(h.recorded()); n != 0 {
		t.Fatalf("CursorPos が失敗した tick で MoveTo が %d 回呼ばれました, want 0", n)
	}
	select {
	case err := <-h.done:
		t.Fatalf("CursorPos の失敗で Run が戻りました: %v", err)
	default:
	}
	if got := h.tracker.LastActivity(); !got.Equal(runnerStart) {
		t.Errorf("LastActivity = %v, want 起動時刻（失敗を操作と見なさない）", got)
	}

	h.advanceTo(t, 10*time.Minute+6*time.Second)
	ls := laps(t, h.recorded())
	if len(ls) != 2 {
		t.Fatalf("10 分までの 1 周の回数 = %d, want 2", len(ls))
	}
	assertLapStart(t, "1 周目", ls[0], 5*time.Minute+4*time.Second, 0)
	assertLapStart(t, "2 周目", ls[1], 10*time.Minute+4*time.Second, time.Second)
	for _, l := range ls {
		assertTargets(t, l, runnerOrigin)
	}
}

// (12) 1 周の途中（ステップの間）で ctx を cancel すると、Run は context.Canceled を返し、それ以降は動かさない（3.3）。
func TestRunnerReturnsOnCancelDuringLap(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, lapOffset(20*time.Millisecond))
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("1 歩目のあとの MoveTo = %d 回, want 1", n)
	}

	h.cancel()
	select {
	case err := <-h.done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run の戻り値 = %v, want context.Canceled", err)
		}
		h.done <- err // 後片付けで再び受け取れるように戻す
	case <-time.After(2 * time.Second):
		t.Fatal("1 周の途中の cancel のあと Run が戻りませんでした")
	}
	h.clk.Advance(time.Minute)
	if n := len(h.mouse.Moves()); n != 1 {
		t.Errorf("cancel のあとの MoveTo の合計 = %d 回, want 1", n)
	}
}

// (13) 1 周の途中の取りやめ判定で CursorPos が失敗したときは、条件 (a)（Tracker の時刻）だけで判定する（2.4, 2.5）。
// 操作が記録されていなければ、位置が変わっていても（取得できないので）1 周を続ける。
func TestRunnerMidLapCursorPosErrorContinuesLap(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, lapOffset(0)) // 1 歩目の MoveTo のあと、StepDelay の待ちに入った
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("1 歩目のあとの MoveTo = %d 回, want 1", n)
	}
	h.mouse.SetPos(Point{X: 800, Y: 600}) // 位置は変わったが、
	h.mouse.FailCursorPos(3, nil)         // 2〜4 歩目の前の判定ではどれも位置を取れない

	h.advanceTo(t, lapOffset(time.Second))
	ls := laps(t, h.recorded())
	if len(ls) != 1 {
		t.Fatalf("1 周の回数 = %d, want 1（位置を取れなくても 1 周を続ける）", len(ls))
	}
	assertTargets(t, ls[0], runnerOrigin)
	if got := h.mouse.Pos(); got != runnerOrigin {
		t.Errorf("1 周のあとの位置 = %v, want %v", got, runnerOrigin)
	}
	if got := h.tracker.LastActivity(); !got.Equal(runnerStart) {
		t.Errorf("LastActivity = %v, want 起動時刻（位置を取れない判定を操作と見なさない）", got)
	}
	select {
	case err := <-h.done:
		t.Fatalf("CursorPos の失敗で Run が戻りました: %v", err)
	default:
	}
}

// (14) 1 周の途中の取りやめ判定で CursorPos が失敗しても、操作が記録されていれば条件 (a) で取りやめる（2.4, 2.5）。
func TestRunnerMidLapCursorPosErrorStillAbortsOnActivity(t *testing.T) {
	h := startRunner(t, runnerOrigin)
	h.advanceTo(t, lapOffset(20*time.Millisecond)) // 1 歩目のあと、StepDelay の途中
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("1 歩目のあとの MoveTo = %d 回, want 1", n)
	}
	h.tracker.MarkActivity(h.clk.Now())
	h.mouse.FailCursorPos(1, nil) // 2 歩目の前の判定では位置を取れない

	h.advanceTo(t, lapOffset(time.Second))
	if n := len(h.recorded()); n != 1 {
		t.Fatalf("操作のあとの MoveTo = %d 回, want 1（条件 (a) で取りやめる）", n)
	}
	if got, want := h.mouse.Pos(), (Point{100, 90}); got != want {
		t.Errorf("取りやめたあとの位置 = %v, want %v", got, want)
	}
}
