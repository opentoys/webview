//go:build windows

package windows

import (
	"errors"
	"fmt"
)

func (p *Platform) ShowDevTools() error {
	if !p.Debug {
		return errors.New("webview: ShowDevTools requires Debug: true")
	}
	p.mu.Lock()
	ready := p.ready.Load() != 0 && !p.closed
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
		if p.webview == nil {
			return
		}
		hr := p.webview.OpenDevToolsWindow()
		if int32(hr) < 0 {
			err = fmt.Errorf("webview: OpenDevToolsWindow failed: 0x%X", hr)
			return
		}
		err = nil
	})
	return err
}
