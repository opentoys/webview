package webview

import (
	"errors"
	"testing"

	"github.com/opentoys/webview/internal/chrome"
)

type devToolsPlatform struct {
	dialogPlatformStub
	calls, dispatched int
	onUI              bool
	err               error
}

func (p *devToolsPlatform) MainThread(fn func()) {
	p.dispatched++
	p.onUI = true
	defer func() { p.onUI = false }()
	fn()
}
func (p *devToolsPlatform) ShowDevTools() error {
	if !p.onUI {
		panic("developer tools outside UI thread")
	}
	p.calls++
	return p.err
}
func TestShowDevTools(t *testing.T) {
	want := errors.New("native failure")
	p := &devToolsPlatform{err: want}
	w := &W{p: p, started: true}
	if err := w.ShowDevTools(); !errors.Is(err, want) {
		t.Fatal(err)
	}
	p.err = nil
	for range 2 {
		if err := w.ShowDevTools(); err != nil {
			t.Fatal(err)
		}
	}
	if p.calls != 3 || p.dispatched != 3 {
		t.Fatalf("calls=%d dispatched=%d", p.calls, p.dispatched)
	}
	w.closed = true
	if err := w.ShowDevTools(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if p.calls != 3 {
		t.Fatal("called closed backend")
	}
}
func TestChromeShowDevTools(t *testing.T) {
	w := &W{p: &chrome.Chrome{}}
	if err := w.ShowDevTools(); !errors.Is(err, ErrDevToolsUnsupported) {
		t.Fatal(err)
	}
}
