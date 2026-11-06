//go:build darwin

package darwin

import (
	"errors"
	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/types"
)

func (p *Platform) ShowDevTools() error {
	if !p.Debug {
		return errors.New("webview: ShowDevTools requires Debug: true")
	}
	p.mu.Lock()
	ready := p.webview != 0 && !p.closed
	p.mu.Unlock()
	err := errors.New("webview: ShowDevTools requires a running window")
	if !ready {
		return err
	}
	p.MainThread(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.closed {
			return
		}
		if p.webview == 0 {
			return
		}
		inspectorSel := objc.RegisterName("_inspector")
		showSel := objc.RegisterName("show")
		if p.webview.Send(respondsToSelectorSel, inspectorSel) == 0 {
			err = types.ErrDevToolsUnsupported
			return
		}
		inspector := p.webview.Send(inspectorSel)
		if inspector == 0 || inspector.Send(respondsToSelectorSel, showSel) == 0 {
			err = types.ErrDevToolsUnsupported
			return
		}
		inspector.Send(showSel)
		err = nil
	})
	return err
}
