//go:build darwin

package darwin

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/bridgejs"
	"github.com/opentoys/webview/internal/types"
)

func TestIndependentNativeLifetimes(t *testing.T) {
	click1, click2 := make(chan struct{}, 2), make(chan struct{}, 2)
	tray1, err := NewTray(types.SystrayOptions{Title: "Lifecycle A", OnClick: func() { click1 <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer tray1.Close()
	tray2, err := NewTray(types.SystrayOptions{Title: "Lifecycle B", OnClick: func() { click2 <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer tray2.Close()
	mainThread(func() {
		if tray1.owner.statusItem == tray2.owner.statusItem {
			t.Error("trays share native identity")
		}
	})
	makeWindow := func(name string) (*Platform, <-chan string) {
		t.Helper()
		p, err := New()
		if err != nil {
			t.Fatal(err)
		}
		messages := make(chan string, 8)
		p.MessageFunc = func(body string) { messages <- body }
		p.BindingScript = func() string { return bridgejs.Proxies(bridgejs.Functions([]string{name})) }
		p.InterceptResource("lifetime", func(req ResourceRequest, respond func(*ResourceResponse)) {
			respond(&ResourceResponse{
				StatusCode: 200,
				Headers:    http.Header{"Content-Type": {"text/html"}},
				Body:       []byte(fmt.Sprintf(`<!doctype html><input id="state" value="%s"><script>window.initialized=(window.initialized||0)+1;webkit.messageHandlers.webviewBridge.postMessage('%s:'+typeof %s)</script>`, name, name, name)),
			})
		})
		if err := p.Navigate("lifetime://window/index.html"); err != nil {
			t.Fatal(err)
		}
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p, messages
	}
	expect := func(messages <-chan string, want string) {
		t.Helper()
		select {
		case got := <-messages:
			if got != want {
				t.Fatalf("message=%q want=%q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("missing message %q", want)
		}
	}
	p1, m1 := makeWindow("firstWindow")
	p2, m2 := makeWindow("secondWindow")
	expect(m1, "firstWindow:function")
	expect(m2, "secondWindow:function")
	if err := p1.Eval(`document.querySelector('#state').value='retained';window.counter=17`); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := p1.Hidden(); err != nil {
			t.Fatal(err)
		}
		if err := p1.Show(); err != nil {
			t.Fatal(err)
		}
	}
	if err := p1.Eval(`webkit.messageHandlers.webviewBridge.postMessage(document.querySelector('#state').value+':'+window.counter+':'+window.initialized+':'+typeof secondWindow)`); err != nil {
		t.Fatal(err)
	}
	expect(m1, "retained:17:1:undefined")
	closeRequested := make(chan struct{}, 1)
	p2.SetCloseRequest(func() { closeRequested <- struct{}{} })
	mainThread(func() { p2.window.Send(objc.RegisterName("performClose:"), objc.ID(0)) })
	select {
	case <-closeRequested:
	case <-time.After(time.Second):
		t.Fatal("native close did not intercept")
	}
	mainThread(func() {
		if p2.window == 0 {
			t.Error("intercepted close destroyed window")
		}
	})
	if err := p1.Close(); err != nil {
		t.Fatal(err)
	}
	mainThread(func() {
		if p1.window != 0 || p1.webview != 0 || p1.dropView != 0 {
			t.Error("Close retained native window/view")
		}
	})
	if err := p2.Eval(`webkit.messageHandlers.webviewBridge.postMessage('second alive')`); err != nil {
		t.Fatal(err)
	}
	expect(m2, "second alive")
	p3, m3 := makeWindow("replacementWindow")
	expect(m3, "replacementWindow:function")
	if err := p2.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p3.Close(); err != nil {
		t.Fatal(err)
	}
	mainThread(func() {
		for _, tray := range []*Tray{tray1, tray2} {
			button := tray.owner.statusItem.Send(objc.RegisterName("button"))
			tray.owner.trayTarget.Send(objc.RegisterName("clicked:"), button)
		}
	})
	select {
	case <-click1:
	case <-time.After(time.Second):
		t.Fatal("first tray lost callback after closing windows")
	}
	select {
	case <-click2:
	case <-time.After(time.Second):
		t.Fatal("second tray lost callback after closing windows")
	}
	if err := tray1.Close(); err != nil {
		t.Fatal(err)
	}
	mainThread(func() {
		if tray2.owner.statusItem == 0 {
			t.Error("closing first tray removed second")
		}
	})
}
