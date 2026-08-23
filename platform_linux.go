//go:build linux

package webview

import (
	"github.com/opentoys/webview/internal/linux"
)

func buildPlatform(opts Options, w *W) Platform {
	p := linux.New()
	p.Debug = opts.Debug
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	return p
}
