//go:build darwin

package webview

import "github.com/opentoys/webview/platform/darwin"

// Type aliases forward the platform package's types so callers don't need
// to import platform/darwin directly.
type DialogKind = darwin.DialogKind

const (
	DialogAlert   = darwin.DialogAlert
	DialogConfirm = darwin.DialogConfirm
	DialogPrompt  = darwin.DialogPrompt
)

type SizeHint = darwin.SizeHint

const (
	SizeNone  = darwin.SizeNone
	SizeMin   = darwin.SizeMin
	SizeMax   = darwin.SizeMax
	SizeFixed = darwin.SizeFixed
)

// buildPlatform creates the platform and wires the message handler to the
// bridge: JS postMessages are parsed, bound Go funcs run, and the JSON reply
// is eval'd back into the webview on the host thread (non-blocking).
func buildPlatform(opts Options, w *W) Platform {
	p := darwin.New()
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	// DialogFunc routes WKUIDelegate calls to W's dialog handler.
	// Runs on the host thread (same as MessageFunc).
	p.DialogFunc = func(kind DialogKind, message, defaultInput string) (string, bool) {
		if w.dialog != nil {
			return w.dialog(kind, message, defaultInput)
		}
		switch kind {
		case DialogConfirm:
			return "", false
		default:
			return defaultInput, true
		}
	}
	// openPanelSet pushes W's handler into the platform. When nil (no
	// SetOpenPanelHandler called), p.OpenPanelFunc stays nil and showOpenPanel
	// runs the native NSOpenPanel — which is the correct default.
	w.openPanelSet = func(fn OpenPanelFunc) {
		if fn != nil {
			p.OpenPanelFunc = func(params darwin.OpenPanelParams, cb func([]string, bool)) {
				fn(OpenPanelParams{
					AllowsMultipleSelection: params.AllowsMultipleSelection,
				}, cb)
			}
		} else {
			p.OpenPanelFunc = nil
		}
	}
	return p
}
