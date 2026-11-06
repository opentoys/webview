//go:build darwin

package darwin

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestShowDevToolsRequiresDebugAndRunningWindow(t *testing.T) {
	p := &Platform{}
	if err := p.ShowDevTools(); err == nil || !strings.Contains(err.Error(), "Debug") {
		t.Fatalf("disabled: %v", err)
	}
	p.Debug = true
	if err := p.ShowDevTools(); err == nil || !strings.Contains(err.Error(), "running window") {
		t.Fatalf("not started: %v", err)
	}
}

func TestShowDevToolsWindow(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	p.Debug = true
	loaded := make(chan string, 4)
	p.MessageFunc = func(s string) { loaded <- s }
	html := "<html><script>webkit.messageHandlers.webviewBridge.postMessage('ready')</script></html>"
	if err := p.SetHTML(html); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	wait := func() {
		t.Helper()
		select {
		case <-loaded:
		case <-time.After(5 * time.Second):
			t.Fatal("page did not load")
		}
	}
	wait()
	for range 2 {
		if err := p.ShowDevTools(); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Navigate("data:text/html," + url.PathEscape(html)); err != nil {
		t.Fatal(err)
	}
	wait()
	if err := p.ShowDevTools(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.ShowDevTools(); err == nil {
		t.Fatal("opened closed window")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.webview != 0 || p.window != 0 {
		t.Fatal("native objects retained after close")
	}
}
