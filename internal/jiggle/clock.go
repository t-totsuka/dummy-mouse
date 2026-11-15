package jiggle

import "time"

// SystemClock は実時間で動く Clock。time.Now と time.After をそのまま呼ぶ。
type SystemClock struct{}

// Now は現在の時刻を返す。
func (SystemClock) Now() time.Time { return time.Now() }

// After は d が経過したら現在の時刻を送るチャネルを返す。
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
