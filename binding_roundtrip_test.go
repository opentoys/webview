package webview

import (
	"io"
	"net/url"
	"testing"
	"time"

	"github.com/opentoys/webview/internal/chrome"
	"github.com/opentoys/webview/internal/debuglog"
)

func TestObjectRoundtripChrome(t *testing.T) {
	if chrome.ChromeExecutable() == "" {
		t.Skip("Chrome is unavailable")
	}
	b := newBridge()
	p := chrome.New(chrome.Options{Offscreen: true})
	p.Logger = debuglog.New(io.Discard)
	p.BindingScript = b.bindingScript
	p.MessageFunc = func(body string) { go b.HandleMessage(body, p.EvalHost) }
	exerciseObjectBackend(t, p, b)
}

// Use a backend directly so these tests do not start or stop the global app.
func exerciseObjectBackend(t *testing.T, p Platform, b *bridge) {
	t.Helper()
	w := &W{p: p, bridge: b}
	defer p.Close()
	reports := make(chan string, 32)
	b.Bind("report", func(s string) { reports <- s })
	b.Bind("app", &bindingObject{})
	if err := p.Init(`"use strict"; window.bindingStrict = (function(){return this === undefined})()`); err != nil {
		t.Fatal(err)
	}
	html := `<!doctype html><meta charset="utf-8"><script>app.Greet('世界').then(v=>report('loaded:'+v+':'+window.bindingStrict)).catch(e=>report('error:'+e));</script>`
	if err := p.SetHTML(`<!doctype html><meta charset="utf-8">`); err != nil {
		t.Fatal(err)
	}
	if err := p.(interface{ Start() error }).Start(); err != nil {
		t.Fatal(err)
	}
	w.started = true
	if err := p.Navigate("data:text/html," + url.PathEscape(html)); err != nil {
		t.Fatal(err)
	}
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-reports:
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %q", want)
		}
	}
	eval := func(js string) {
		t.Helper()
		if err := p.Eval(js); err != nil {
			t.Fatal(err)
		}
	}
	bind := func(value any) {
		t.Helper()
		if err := w.Bind("app", value); err != nil {
			t.Fatal(err)
		}
	}
	expect("loaded:hi 世界:true")
	eval(`window.savedApp=app; report('saved')`)
	expect("saved")
	bind(bindingOther{})
	eval(`report(Object.keys(app).join(',')); savedApp.Greet('x').then(()=>report('wrong'),()=>report('old rejected')); void 0`)
	expect("Other")
	expect("old rejected")
	bind(func() string { return "function" })
	eval(`app().then(report); void 0`)
	expect("function")
	bind(&bindingObject{})
	if err := p.Navigate("data:text/html," + url.PathEscape(html)); err != nil {
		t.Fatal(err)
	}
	expect("loaded:hi 世界:true")
	bind(struct{ Version string }{Version: "struct"})
	eval(`report(app.Version + ':' + Object.isFrozen(app))`)
	expect("struct:true")
	values := map[string]any{
		"version": "1.0",
		"nested":  map[string]any{"n": 1},
		"greet":   func() string { return "map function" },
	}
	bind(values)
	values["version"] = "changed"
	eval(`(() => {
 'use strict';
 let readonly = false;
 try {
  app.version = 'other';
 } catch (e) {
  readonly = e instanceof TypeError;
 }
 report(app.version + ':' + readonly + ':' + Object.isFrozen(app.nested));
})();
app.greet().then(report);
void 0`)
	expect("1.0:true:true")
	expect("map function")
	if err := p.Navigate("data:text/html," + url.PathEscape(`<!doctype html><script>report(app.version+':'+Object.isFrozen(app.nested));app.greet().then(report)</script>`)); err != nil {
		t.Fatal(err)
	}
	expect("1.0:true")
	expect("map function")
	entered, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	if err := w.Bind("slow", func() string {
		close(entered)
		<-release
		return "finished"
	}); err != nil {
		t.Fatal(err)
	}
	eval(`window.waiting=slow(); void 0`)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow call did not start")
	}
	bind(bindingOther{})
	if err := w.Unbind("slow"); err != nil {
		t.Fatal(err)
	}
	eval(`report(typeof window.slow); waiting.then(report); void 0`)
	expect("undefined")
	close(release)
	released = true
	expect("finished")
	if err := w.Unbind("app"); err != nil {
		t.Fatal(err)
	}
	eval(`report(typeof window.app)`)
	expect("undefined")
	if err := p.Navigate("data:text/html," + url.PathEscape(`<!doctype html><script>report(typeof window.app)</script>`)); err != nil {
		t.Fatal(err)
	}
	expect("undefined")
}
