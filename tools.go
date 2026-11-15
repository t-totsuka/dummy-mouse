//go:build tools

// Package tools は、コードからまだ import されていない依存を go.mod に固定するためのファイルである。
// ビルドタグ tools が付いているため、通常のビルドには含まれない。
package tools

import (
	_ "fyne.io/systray"
	_ "golang.org/x/sys/windows"
)
