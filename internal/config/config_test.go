package config

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// 4.3: 指定がなければ既定値（5 分 / 10px）を使う。
func TestParseDefaults(t *testing.T) {
	for _, args := range [][]string{nil, {}} {
		c, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", args, err)
		}
		if c.IdleThreshold != 5*time.Minute || c.StepPixels != 10 {
			t.Fatalf("Parse(%q) = %+v, want 5m / 10px", args, c)
		}
		if c.IdleThreshold != DefaultIdleThreshold || c.StepPixels != DefaultStepPixels {
			t.Fatalf("Parse(%q) = %+v, want defaults", args, c)
		}
	}
}

// 4.1, 4.2: 指定された値を使う。
func TestParseAppliesValues(t *testing.T) {
	c, err := Parse([]string{"-idle", "90s", "-step", "3"})
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if c.IdleThreshold != 90*time.Second {
		t.Errorf("IdleThreshold = %v, want 90s", c.IdleThreshold)
	}
	if c.StepPixels != 3 {
		t.Errorf("StepPixels = %d, want 3", c.StepPixels)
	}
}

func TestParseAppliesValuesEqualsForm(t *testing.T) {
	c, err := Parse([]string{"-idle=1h", "--step=25"})
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if c.IdleThreshold != time.Hour || c.StepPixels != 25 {
		t.Fatalf("Parse = %+v, want 1h / 25px", c)
	}
}

// 4.4: 不正な入力は、利用者向けの日本語メッセージの error にする。
func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string // メッセージに含まれるべき部分
	}{
		{"idle zero", []string{"-idle", "0"}, "-idle は 0 より大きい時間で指定してください（指定値: 0）"},
		{"idle unparsable", []string{"-idle", "abc"}, `-idle の値 "abc" を時間として解釈できません（例: 5m, 90s）`},
		{"step zero", []string{"-step", "0"}, "-step は 1 以上の整数で指定してください（指定値: 0）"},
		{"step negative", []string{"-step", "-5"}, "-step は 1 以上の整数で指定してください（指定値: -5）"},
		{"unknown flag", []string{"-foo"}, "不明なオプションです: -foo"},
		{"extra positional arg", []string{"-idle", "5m", "extra"}, "余分な引数があります: extra"},
		{"idle negative", []string{"-idle", "-5m"}, "-idle は 0 より大きい時間で指定してください（指定値: -5m）"},
		{"step unparsable", []string{"-step", "x"}, `-step の値 "x" を整数として解釈できません`},
		{"missing value", []string{"-step"}, "-step の値が指定されていません"},
		{"help", []string{"-h"}, "使い方: mousemover [-idle 時間] [-step px]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse(tt.args)
			if err == nil {
				t.Fatalf("Parse(%q) = %+v, want error", tt.args, c)
			}
			msg := err.Error()
			if !strings.Contains(msg, tt.want) {
				t.Fatalf("Parse(%q) error = %q, want it to contain %q", tt.args, msg, tt.want)
			}
			if !containsJapanese(msg) {
				t.Fatalf("error %q is not a Japanese message", msg)
			}
			for _, english := range []string{"invalid value", "flag provided but not defined", "needs an argument", "Usage"} {
				if strings.Contains(msg, english) {
					t.Fatalf("error %q contains stdlib English text %q", msg, english)
				}
			}
		})
	}
}

// 3.4: ツールチップの文言に、動作中であることと設定値が入る。
func TestSummary(t *testing.T) {
	tests := []struct {
		c    Config
		want string
	}{
		{Config{IdleThreshold: 5 * time.Minute, StepPixels: 10}, "Idle Mouse Mover: 動作中\nアイドル 5分 / 移動 10px"},
		{Config{IdleThreshold: 90 * time.Second, StepPixels: 3}, "Idle Mouse Mover: 動作中\nアイドル 1分30秒 / 移動 3px"},
		{Config{IdleThreshold: time.Hour, StepPixels: 1}, "Idle Mouse Mover: 動作中\nアイドル 1時間 / 移動 1px"},
		{Config{IdleThreshold: 45 * time.Second, StepPixels: 10}, "Idle Mouse Mover: 動作中\nアイドル 45秒 / 移動 10px"},
		{Config{IdleThreshold: 2*time.Hour + 5*time.Second, StepPixels: 10}, "Idle Mouse Mover: 動作中\nアイドル 2時間5秒 / 移動 10px"},
		{Config{IdleThreshold: 1500 * time.Millisecond, StepPixels: 10}, "Idle Mouse Mover: 動作中\nアイドル 1.5秒 / 移動 10px"},
	}
	for _, tt := range tests {
		got := tt.c.Summary()
		if got != tt.want {
			t.Errorf("Summary(%+v) = %q, want %q", tt.c, got, tt.want)
		}
		// Windows のツールチップは 128 文字（UTF-16、終端を含む）まで。
		if n := utf8.RuneCountInString(got); n >= 128 {
			t.Errorf("Summary length = %d, want < 128", n)
		}
	}
}

func TestSummaryFromParsedDefaults(t *testing.T) {
	c, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	s := c.Summary()
	for _, want := range []string{"動作中", "5分", "10px"} {
		if !strings.Contains(s, want) {
			t.Errorf("Summary = %q, want it to contain %q", s, want)
		}
	}
}

func TestSummaryLengthWithLargeValues(t *testing.T) {
	c := Config{IdleThreshold: time.Duration(1<<63 - 1), StepPixels: int(^uint(0) >> 1)}
	if n := utf8.RuneCountInString(c.Summary()); n >= 128 {
		t.Fatalf("Summary length = %d, want < 128", n)
	}
}

func containsJapanese(s string) bool {
	for _, r := range s {
		if (r >= 0x3040 && r <= 0x30FF) || (r >= 0x4E00 && r <= 0x9FFF) {
			return true
		}
	}
	return false
}
