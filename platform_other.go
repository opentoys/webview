//go:build !darwin && !windows

package webview

import "errors"

// Mirror the darwin package's type/const definitions so the common file
// (webview.go) compiles without importing platform/darwin.
type SizeHint int

const (
	SizeNone SizeHint = iota
	SizeMin
	SizeMax
	SizeFixed
)

type ResourceRequest struct {
	URL     string
	Method  string
	Headers map[string]string
}

type ResourceResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

type ResourceHandler func(req ResourceRequest, respond func(ResourceResponse))

var errUnsupported = errors.New("webview: unsupported platform")

func buildPlatform(opts Options, w *W) Platform {
	panic("webview: unsupported platform; only darwin and windows are implemented")
}
