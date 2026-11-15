package jiggle

import (
	"sync/atomic"
	"time"
)

// Tracker は最後のユーザー操作の時刻を保持する。
//
// フックのコールバック（専用の OS スレッド）と Runner の goroutine から同時に
// 呼ばれるため、ロックを取らず atomic.Int64（UnixNano）で値を持つ。
// 保持する時刻は単調で、LastActivity は決して戻らない。
type Tracker struct {
	last atomic.Int64 // 最後の操作の時刻（UnixNano）
}

// NewTracker は now（起動した時刻）を最後の操作の時刻として初期化した Tracker を返す。
func NewTracker(now time.Time) *Tracker {
	t := &Tracker{}
	t.last.Store(now.UnixNano())
	return t
}

// MarkActivity は at を操作の時刻として記録する。
// 保持している時刻より古い（または同じ）時刻は無視する。CAS ループで更新するためロックは取らない。
func (t *Tracker) MarkActivity(at time.Time) {
	n := at.UnixNano()
	for {
		cur := t.last.Load()
		if n <= cur || t.last.CompareAndSwap(cur, n) {
			return
		}
	}
}

// LastActivity は最後の操作の時刻を返す。
//
// 時刻は UnixNano から time.Unix で組み立て直すため、モノトニック時計の読みと
// ロケーションの情報は失われる（ローカル時刻として返る）。比較には == ではなく
// Equal / Before / Sub を使うこと。
func (t *Tracker) LastActivity() time.Time {
	return time.Unix(0, t.last.Load())
}
