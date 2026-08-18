package webview

import "github.com/opentoys/webview/platform/darwin"

// buildPlatform creates the platform and wires the message handler to the
// bridge: JS postMessages are parsed, bound Go funcs run, and the JSON reply
// is eval'd back into the webview on the host thread (non-blocking).
func buildPlatform(opts Options, w *W) Platform {
	p := darwin.New()
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
	return p
}
