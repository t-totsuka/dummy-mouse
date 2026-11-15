package jiggle

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

var (
	_ Mouse = (*fakeMouse)(nil)
	_ Clock = (*fakeClock)(nil)
	_ Clock = SystemClock{}
)

func TestDefaults(t *testing.T) {
	if DefaultPollInterval != time.Second {
		t.Errorf("DefaultPollInterval = %v, want 1s", DefaultPollInterval)
	}
	if DefaultStepDelay != 50*time.Millisecond {
		t.Errorf("DefaultStepDelay = %v, want 50ms", DefaultStepDelay)
	}
	s := Settings{IdleThreshold: 5 * time.Minute, StepPixels: 10, PollInterval: DefaultPollInterval, StepDelay: DefaultStepDelay}
	if s.IdleThreshold != 5*time.Minute || s.StepPixels != 10 || s.PollInterval != time.Second || s.StepDelay != 50*time.Millisecond {
		t.Errorf("Settings fields not preserved: %+v", s)
	}
}

func TestSystemClock(t *testing.T) {
	var c Clock = SystemClock{}
	a := c.Now()
	b := c.Now()
	if b.Before(a) {
		t.Fatalf("SystemClock.Now went backwards: %v then %v", a, b)
	}
	select {
	case got := <-c.After(time.Millisecond):
		if got.Before(a) {
			t.Fatalf("After fired with time %v before start %v", got, a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SystemClock.After(1ms) did not fire within 2s")
	}
}

func TestFakeClockAdvanceFiresTimers(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newFakeClock(start)
	if !c.Now().Equal(start) {
		t.Fatalf("Now = %v, want %v", c.Now(), start)
	}
	ch1 := c.After(time.Second)
	ch2 := c.After(3 * time.Second)
	if c.Pending() != 2 {
		t.Fatalf("Pending = %d, want 2", c.Pending())
	}

	c.Advance(999 * time.Millisecond)
	select {
	case <-ch1:
		t.Fatal("timer fired before its deadline")
	default:
	}

	c.Advance(time.Millisecond)
	select {
	case got := <-ch1:
		if want := start.Add(time.Second); !got.Equal(want) {
			t.Fatalf("fired with %v, want %v", got, want)
		}
	default:
		t.Fatal("timer did not fire at its deadline")
	}
	if c.Pending() != 1 {
		t.Fatalf("Pending = %d, want 1", c.Pending())
	}

	c.Advance(5 * time.Second)
	select {
	case <-ch2:
	default:
		t.Fatal("second timer did not fire")
	}
	if c.Pending() != 0 {
		t.Fatalf("Pending = %d, want 0", c.Pending())
	}
	if want := start.Add(6 * time.Second); !c.Now().Equal(want) {
		t.Fatalf("Now = %v, want %v", c.Now(), want)
	}

	select {
	case <-c.After(0):
	default:
		t.Fatal("After(0) did not fire immediately")
	}
}

func TestFakeClockBlockUntilSyncsWithGoroutine(t *testing.T) {
	c := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	done := make(chan time.Time)
	go func() {
		done <- <-c.After(time.Second)
	}()
	c.BlockUntil(t, 1)
	c.Advance(time.Second)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine was not woken by Advance")
	}
}

func TestFakeMouse(t *testing.T) {
	bounds := Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	m := newFakeMouse(Point{100, 100}, bounds)

	if b, err := m.Bounds(); err != nil || b != bounds {
		t.Fatalf("Bounds = %v, %v", b, err)
	}
	if err := m.MoveTo(Point{100, 90}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if p, err := m.CursorPos(); err != nil || p != (Point{100, 90}) {
		t.Fatalf("CursorPos after MoveTo = %v, %v", p, err)
	}

	m.SetPos(Point{500, 500})
	if p, _ := m.CursorPos(); p != (Point{500, 500}) {
		t.Fatalf("CursorPos after SetPos = %v", p)
	}

	boom := errors.New("boom")
	m.FailMoveTo(1, boom)
	if err := m.MoveTo(Point{1, 1}); !errors.Is(err, boom) {
		t.Fatalf("MoveTo err = %v, want boom", err)
	}
	if m.Pos() != (Point{500, 500}) {
		t.Fatalf("failed MoveTo changed position to %v", m.Pos())
	}
	if err := m.MoveTo(Point{2, 2}); err != nil {
		t.Fatalf("MoveTo after injected failure: %v", err)
	}

	m.FailCursorPos(2, nil)
	for i := 0; i < 2; i++ {
		if _, err := m.CursorPos(); !errors.Is(err, errFakeMouse) {
			t.Fatalf("CursorPos #%d err = %v, want errFakeMouse", i, err)
		}
	}
	if p, err := m.CursorPos(); err != nil || p != (Point{2, 2}) {
		t.Fatalf("CursorPos after failures = %v, %v", p, err)
	}

	m.SetBoundsErr(boom)
	if _, err := m.Bounds(); !errors.Is(err, boom) {
		t.Fatalf("Bounds err = %v, want boom", err)
	}
	m.SetBoundsErr(nil)

	calls := 0
	m.SetOnMoveTo(func(p Point) error {
		calls++
		if p == (Point{9, 9}) {
			return boom
		}
		return nil
	})
	if err := m.MoveTo(Point{9, 9}); !errors.Is(err, boom) {
		t.Fatalf("hook err = %v, want boom", err)
	}
	if err := m.MoveTo(Point{3, 3}); err != nil {
		t.Fatalf("MoveTo with passing hook: %v", err)
	}
	if calls != 2 || m.Pos() != (Point{3, 3}) {
		t.Fatalf("calls = %d, pos = %v", calls, m.Pos())
	}

	want := []Point{{100, 90}, {1, 1}, {2, 2}, {9, 9}, {3, 3}}
	if got := m.Moves(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Moves = %v, want %v", got, want)
	}
}
