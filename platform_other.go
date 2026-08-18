//go:build !darwin

package webview

import "errors"

// Mirror the darwin package's type/const definitions so the common file
// (webview.go) compiles without importing platform/darwin.
type DialogKind int

const (
	DialogAlert DialogKind = iota
	DialogConfirm
	DialogPrompt
)

type SizeHint int

const (
	SizeNone SizeHint = iota
	SizeMin
	SizeMax
	SizeFixed
)

var errUnsupported = errors.New("webview: unsupported platform")

func buildPlatform(opts Options, w *W) Platform {
	panic("webview: unsupported platform; only darwin is implemented")
}
