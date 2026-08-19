//go:build windows

package webview

import win "github.com/opentoys/webview/platform/windows"

// Type aliases forward the platform package's types so callers don't need
// to import platform/windows directly.
type DialogKind = win.DialogKind

const (
	DialogAlert   = win.DialogAlert
	DialogConfirm = win.DialogConfirm
	DialogPrompt  = win.DialogPrompt
)

type SizeHint = win.SizeHint

const (
	SizeNone  = win.SizeNone
	SizeMin   = win.SizeMin
	SizeMax   = win.SizeMax
	SizeFixed = win.SizeFixed
)

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
	// openPanelSet pushes W's handler into the platform.
	w.openPanelSet = func(fn OpenPanelFunc) {
		if fn != nil {
			p.OpenPanelFunc = func(params win.OpenPanelParams, cb func([]string, bool)) {
				fn(OpenPanelParams{
					AllowsMultipleSelection: params.AllowsMultipleSelection,
					AllowsDirectories:       params.AllowsDirectories,
				}, cb)
			}
		} else {
			p.OpenPanelFunc = nil
		}
	}
	// downloadSet pushes W's handler into the platform.
	w.downloadSet = func(fn DownloadFunc) {
		if fn != nil {
			p.DownloadFunc = func(name string, cb func(string)) {
				fn(name, cb)
			}
		} else {
			p.DownloadFunc = nil
		}
	}
	return p
}
