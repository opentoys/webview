//go:build darwin

package webview

import (
	"github.com/opentoys/webview/internal/darwin"
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

func buildPlatform(opts Options, w *W) Platform {
	p := darwin.New()
	p.Debug = opts.Debug
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	// Capture <input accept> on click so the native file picker can read it
	// synchronously before showing the panel.
	w.bridge.Bind("__accept__", func(v string) {
		darwin.SetAccept(v)
	})
	p.Init(`document.addEventListener('click', function(e) {
		var el = e.target;
		if (el && el.tagName === 'INPUT' && el.type === 'file') {
			__accept__(el.accept || '');
		}
	}, true);`)
	return p
}
