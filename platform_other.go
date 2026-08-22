//go:build !darwin && !windows && !linux

package webview

import (
	"errors"

	"github.com/opentoys/webview/types"
)

type SizeHint = types.SizeHint

const (
	SizeNone  = types.SizeNone
	SizeMin   = types.SizeMin
	SizeMax   = types.SizeMax
	SizeFixed = types.SizeFixed
)

type ResourceRequest = types.ResourceRequest
type ResourceResponse = types.ResourceResponse
type ResourceHandler = types.ResourceHandler

var errUnsupported = errors.New("webview: unsupported platform")

func buildPlatform(opts Options, w *W) Platform {
	panic("webview: unsupported platform; only darwin and windows are implemented")
}
