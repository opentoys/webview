//go:build linux

package webview

import (
	"github.com/opentoys/webview/internal/debuglog"
	"github.com/opentoys/webview/internal/linux"
)

func buildPlatform(opts Options, w *W, logger *debuglog.Logger) (Platform, error) {
	p, e := linux.New()
	if e != nil {
		return nil, e
	}
	p.Debug = opts.Debug
	p.Logger = logger
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.Offscreen = opts.Offscreen
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	// Capture <input accept> on click so the native file picker can read it
	// synchronously before showing the dialog.
	w.bridge.Bind("__accept__", func(v string) {
		linux.SetFileAccept(v)
	})
	p.Init(`document.addEventListener('click', function(e) {
		var el = e.target;
		if (el && el.tagName === 'INPUT' && el.type === 'file') {
			__accept__(el.accept || '');
		}
	}, true);`)
	return p, nil
}
