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
	return p
}
