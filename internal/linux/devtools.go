//go:build linux

package linux

import (
	"errors"
)

func (p *gtk) ShowDevTools() error {
	if !p.Debug {
		return errors.New("webview: ShowDevTools requires Debug: true")
	}
	p.mu.Lock()
	ready := p.uiTID.Load() != 0 && !p.extensionsClosed
	p.mu.Unlock()
	err := errors.New("webview: ShowDevTools requires a running window")
	if !ready {
		return err
	}
	p.MainThread(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.extensionsClosed {
			return
		}
		if p.webview == 0 {
			return
		}
		inspector := webkitWebViewGetInspector(p.webview)
		if inspector == 0 {
			err = errors.New("webview: developer tools inspector unavailable")
			return
		}
		webkitWebInspectorShow(inspector)
		err = nil
	})
	return err
}
