//go:build windows

package webview

import win "github.com/opentoys/webview/platform/windows"

type SizeHint = win.SizeHint

const (
	SizeNone  = win.SizeNone
	SizeMin   = win.SizeMin
	SizeMax   = win.SizeMax
	SizeFixed = win.SizeFixed
)

type SchemeRequest = win.SchemeRequest
type SchemeResponse = win.SchemeResponse
type SchemeHandler = win.SchemeHandler

// buildPlatform creates the platform and wires the message handler to the
// bridge: JS postMessages are parsed, bound Go funcs run, and the JSON reply
// is eval'd back into the webview on the host thread (non-blocking).
func buildPlatform(opts Options, w *W) Platform {
	p := win.New()
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.Debug = opts.Debug
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	return p
}
