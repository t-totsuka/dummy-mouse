package jiggle

import (
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeClock は手動で時間を進める Clock のテスト用実装。
// テスト対象の goroutine とテストの goroutine の両方から安全に使える。
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	deadline time.Time
	ch       chan time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After は Advance で時刻が now+d 以上になったときに発火するチャネルを返す。
// d <= 0 のときはすぐに発火する。
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.timers = append(c.timers, &fakeTimer{deadline: c.now.Add(d), ch: ch})
	return ch
}

// Advance は時刻を d だけ進め、期限に達したタイマーを期限の早い順に発火させる。
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	sort.SliceStable(c.timers, func(i, j int) bool {
		return c.timers[i].deadline.Before(c.timers[j].deadline)
	})
	var remaining []*fakeTimer
	for _, t := range c.timers {
		if !t.deadline.After(c.now) {
			t.ch <- c.now // バッファ 1 で 1 回だけ送るのでブロックしない
			continue
		}
		remaining = append(remaining, t)
	}
	c.timers = remaining
}

// Pending は未発火のタイマーの数を返す。
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// BlockUntil は未発火のタイマーが n 個以上になるまで待つ。
// 一定時間内にそうならなければテストを失敗させる（ハングさせない）。
// テストの goroutine から呼ぶこと（t.Fatalf を使うため）。
func (c *fakeClock) BlockUntil(t testing.TB, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if c.Pending() >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fakeClock.BlockUntil: 未発火のタイマーが %d 個になりませんでした（現在 %d 個）", n, c.Pending())
		}
		time.Sleep(time.Millisecond)
	}
}

// errFakeMouse は fakeMouse に注入するエラーの既定値。
var errFakeMouse = errors.New("fakeMouse: injected error")

// fakeMouse は座標と呼び出し履歴を記録し、エラーを注入できる Mouse のテスト用実装。
type fakeMouse struct {
	mu     sync.Mutex
	pos    Point
	bounds Rect
	moves  []Point

	moveFailures int   // 次の N 回の MoveTo を失敗させる
	posFailures  int   // 次の N 回の CursorPos を失敗させる
	moveErr      error // 失敗時に返すエラー（nil なら errFakeMouse）
	posErr       error
	boundsErr    error

	// onMoveTo が非 nil なら MoveTo の呼び出しごとに（ロックの外で）呼ばれる。
	// 非 nil の error を返すとその MoveTo は失敗し、位置は変わらない。
	onMoveTo func(p Point) error
}

func newFakeMouse(pos Point, bounds Rect) *fakeMouse {
	return &fakeMouse{pos: pos, bounds: bounds}
}

func (m *fakeMouse) CursorPos() (Point, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.posFailures > 0 {
		m.posFailures--
		return Point{}, orFakeErr(m.posErr)
	}
	return m.pos, nil
}

// MoveTo は呼び出しを履歴に記録し、成功したら位置を p にする（事後条件: CursorPos() == p）。
func (m *fakeMouse) MoveTo(p Point) error {
	m.mu.Lock()
	hook := m.onMoveTo
	m.mu.Unlock()
	var hookErr error
	if hook != nil {
		hookErr = hook(p)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.moves = append(m.moves, p)
	if hookErr != nil {
		return hookErr
	}
	if m.moveFailures > 0 {
		m.moveFailures--
		return orFakeErr(m.moveErr)
	}
	m.pos = p
	return nil
}

func (m *fakeMouse) Bounds() (Rect, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.boundsErr != nil {
		return Rect{}, m.boundsErr
	}
	return m.bounds, nil
}

// SetPos はユーザーがカーソルを動かしたことを模擬する（MoveTo の履歴には残らない）。
func (m *fakeMouse) SetPos(p Point) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pos = p
}

// Pos は現在の位置を返す（エラー注入の影響を受けない）。
func (m *fakeMouse) Pos() Point {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pos
}

// Moves は MoveTo に渡された座標の履歴（失敗した呼び出しも含む）のコピーを返す。
func (m *fakeMouse) Moves() []Point {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Point(nil), m.moves...)
}

// FailMoveTo は次の n 回の MoveTo を err で失敗させる（err が nil なら errFakeMouse）。
func (m *fakeMouse) FailMoveTo(n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.moveFailures, m.moveErr = n, err
}

// FailCursorPos は次の n 回の CursorPos を err で失敗させる（err が nil なら errFakeMouse）。
func (m *fakeMouse) FailCursorPos(n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.posFailures, m.posErr = n, err
}

// SetBoundsErr は Bounds が返すエラーを設定する（nil で解除）。
func (m *fakeMouse) SetBoundsErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.boundsErr = err
}

// SetOnMoveTo は MoveTo の呼び出しごとのフックを設定する（nil で解除）。
// フックはロックの外で呼ばれるので、中で SetPos などを呼んでよい。
func (m *fakeMouse) SetOnMoveTo(f func(p Point) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onMoveTo = f
}

func orFakeErr(err error) error {
	if err == nil {
		return errFakeMouse
	}
	return err
}
