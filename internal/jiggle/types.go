// Package jiggle は、アイドル判定とカーソル移動の OS に依存しないドメインロジックを提供する。
package jiggle

import "time"

// 動作設定の固定値。
const (
	// DefaultPollInterval はアイドル判定のポーリング間隔。
	DefaultPollInterval = 1 * time.Second
	// DefaultStepDelay は 1 周の各ステップの間隔（各移動をアプリに確実に届けるため）。
	DefaultStepDelay = 50 * time.Millisecond
)

// Point は仮想スクリーン座標上の点。
type Point struct{ X, Y int }

// Rect は仮想スクリーンの範囲。Left/Top を含み、Right/Bottom は含まない。
type Rect struct{ Left, Top, Right, Bottom int }

// Mouse はカーソルの位置の取得と移動の抽象。
type Mouse interface {
	CursorPos() (Point, error)
	// MoveTo はユーザーの入力と同じ扱いになる方法で p へ移動する。
	// 事後条件: 成功したら CursorPos() == p。
	MoveTo(p Point) error
	Bounds() (Rect, error)
}

// Clock は時刻と待ち時間の抽象。ドメインのロジックは time.Now を直接呼ばずにこれを使う。
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Settings は Runner の動作設定。
type Settings struct {
	IdleThreshold time.Duration // config から受け取る
	StepPixels    int           // config から受け取る
	PollInterval  time.Duration // 固定値 1s（DefaultPollInterval）
	StepDelay     time.Duration // 固定値 50ms（DefaultStepDelay）
}
