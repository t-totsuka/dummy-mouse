package jiggle

import (
	"sync"
	"testing"
	"time"
)

var trackerBase = time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)

func TestTrackerNewInitializesLastActivity(t *testing.T) {
	tr := NewTracker(trackerBase)
	if got := tr.LastActivity(); !got.Equal(trackerBase) {
		t.Fatalf("LastActivity() = %v, want %v", got, trackerBase)
	}
}

func TestTrackerMarkActivityAdvances(t *testing.T) {
	tr := NewTracker(trackerBase)
	later := trackerBase.Add(4 * time.Minute)
	tr.MarkActivity(later)
	if got := tr.LastActivity(); !got.Equal(later) {
		t.Fatalf("LastActivity() = %v, want %v", got, later)
	}
}

func TestTrackerMarkActivityIgnoresOlderTime(t *testing.T) {
	tr := NewTracker(trackerBase)
	later := trackerBase.Add(time.Minute)
	tr.MarkActivity(later)
	tr.MarkActivity(trackerBase.Add(30 * time.Second))
	if got := tr.LastActivity(); !got.Equal(later) {
		t.Fatalf("after older mark: LastActivity() = %v, want %v", got, later)
	}
	tr.MarkActivity(trackerBase.Add(-time.Hour))
	if got := tr.LastActivity(); !got.Equal(later) {
		t.Fatalf("after time before init: LastActivity() = %v, want %v", got, later)
	}
}

func TestTrackerConcurrentMarkAndRead(t *testing.T) {
	tr := NewTracker(trackerBase)
	const goroutines = 16
	const perG = 500

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan string, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			prev := tr.LastActivity()
			for i := 0; i < perG; i++ {
				// 各 goroutine は時刻を前後に揺らしながら記録する。
				off := time.Duration(i*goroutines+g) * time.Millisecond
				if i%2 == 1 {
					off -= 50 * time.Millisecond
				}
				tr.MarkActivity(trackerBase.Add(off))
				cur := tr.LastActivity()
				if cur.Before(prev) {
					errs <- "LastActivity moved backwards"
					return
				}
				prev = cur
			}
		}(g)
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}

	want := trackerBase.Add(time.Duration((perG-2)*goroutines+goroutines-1) * time.Millisecond)
	if got := tr.LastActivity(); !got.Equal(want) {
		t.Fatalf("final LastActivity() = %v, want %v (max of all marks)", got, want)
	}
}
