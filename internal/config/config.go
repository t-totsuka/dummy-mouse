// Package config は、起動オプションの解釈と検証を行う。
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultIdleThreshold は、-idle が指定されていないときのアイドル時間。
	DefaultIdleThreshold = 5 * time.Minute
	// DefaultStepPixels は、-step が指定されていないときの移動量（px）。
	DefaultStepPixels = 10
)

// usage は、-h / -help のときと、エラーの補足に使う使い方の説明。
const usage = "使い方: mousemover [-idle 時間] [-step px]\n" +
	"  -idle  操作がない状態がこの時間続いたらカーソルを動かす（例: 5m, 90s, 1h。既定値: 5m）\n" +
	"  -step  1 回に動かす量（px、1 以上の整数。既定値: 10）"

// Config は、検証済みの起動設定。
type Config struct {
	IdleThreshold time.Duration // > 0
	StepPixels    int           // > 0
}

// parseIdle は -idle の値を解釈・検証し、日本語のエラーを返す。
func parseIdle(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("-idle の値 %q を時間として解釈できません（例: 5m, 90s）", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("-idle は 0 より大きい時間で指定してください（指定値: %s）", s)
	}
	return d, nil
}

// parseStep は -step の値を解釈・検証し、日本語のエラーを返す。
func parseStep(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("-step の値 %q を整数として解釈できません（1 以上の整数で指定してください）", s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("-step は 1 以上の整数で指定してください（指定値: %d）", n)
	}
	return n, nil
}

// Parse は os.Args[1:] を受け取る。
// 失敗したときは、そのまま利用者に見せられる日本語のメッセージを持つ error を返す。
func Parse(args []string) (Config, error) {
	c := Config{IdleThreshold: DefaultIdleThreshold, StepPixels: DefaultStepPixels}

	fs := flag.NewFlagSet("mousemover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	// flag パッケージは値のエラーを英語の文に包んでしまうため、
	// 元の日本語のエラーをここで受け取っておき、そのまま返す。
	var valueErr error
	fs.Func("idle", "アイドル時間（例: 5m, 90s）", func(s string) error {
		d, err := parseIdle(s)
		if err != nil {
			valueErr = err
			return err
		}
		c.IdleThreshold = d
		return nil
	})
	fs.Func("step", "移動量（px）", func(s string) error {
		n, err := parseStep(s)
		if err != nil {
			valueErr = err
			return err
		}
		c.StepPixels = n
		return nil
	})

	if err := fs.Parse(args); err != nil {
		return Config{}, translateParseError(err, valueErr)
	}
	if rest := fs.Args(); len(rest) > 0 {
		return Config{}, fmt.Errorf("余分な引数があります: %s\n%s", strings.Join(rest, " "), usage)
	}
	return c, nil
}

// translateParseError は、flag パッケージのエラーを日本語のメッセージにする。
func translateParseError(err, valueErr error) error {
	if valueErr != nil {
		return valueErr
	}
	if errors.Is(err, flag.ErrHelp) {
		return errors.New(usage)
	}
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "flag provided but not defined: "); ok {
		return fmt.Errorf("不明なオプションです: %s\n%s", name, usage)
	}
	if name, ok := strings.CutPrefix(msg, "flag needs an argument: "); ok {
		return fmt.Errorf("%s の値が指定されていません\n%s", name, usage)
	}
	if name, ok := strings.CutPrefix(msg, "bad flag syntax: "); ok {
		return fmt.Errorf("オプションの書き方が正しくありません: %s\n%s", name, usage)
	}
	return fmt.Errorf("起動オプションを解釈できません\n%s", usage)
}

// Summary はツールチップ用の文言を返す。例: "Idle Mouse Mover: 動作中\nアイドル 5分 / 移動 10px"
func (c Config) Summary() string {
	return fmt.Sprintf("Idle Mouse Mover: 動作中\nアイドル %s / 移動 %dpx", formatDuration(c.IdleThreshold), c.StepPixels)
}

// formatDuration は、時間を日本語の読みやすい形にする（例: 5分、1分30秒、1時間）。
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return d.String()
	}
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute

	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%d時間", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%d分", m)
	}
	if d > 0 {
		b.WriteString(strconv.FormatFloat(d.Seconds(), 'f', -1, 64))
		b.WriteString("秒")
	}
	return b.String()
}
