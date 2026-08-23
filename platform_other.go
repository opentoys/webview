//go:build !darwin && !windows && !linux

package webview

import (
	"errors"
)

var errUnsupported = errors.New("webview: unsupported platform")

func buildPlatform(opts Options, w *W) Platform {
	panic("webview: unsupported platform; only darwin and windows are implemented")
}
