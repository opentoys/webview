//go:build darwin

package webview

import "github.com/opentoys/webview/platform/darwin"

type SizeHint = darwin.SizeHint

const (
	SizeNone  = darwin.SizeNone
	SizeMin   = darwin.SizeMin
	SizeMax   = darwin.SizeMax
	SizeFixed = darwin.SizeFixed
)

type ResourceRequest = darwin.ResourceRequest
type ResourceResponse = darwin.ResourceResponse
type ResourceHandler = darwin.ResourceHandler

func buildPlatform(opts Options, w *W) Platform {
	p := darwin.New()
	p.Debug = opts.Debug
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	return p
}
