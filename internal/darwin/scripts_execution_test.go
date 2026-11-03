//go:build darwin

package darwin

import (
	"encoding/json"
	"fmt"
	"github.com/ebitengine/purego/objc"
	"net/url"
	"testing"
	"time"
)

func TestInitAndEvalExecution(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	messages := make(chan string, 16)
	p.MessageFunc = func(body string) { messages <- body }
	p.BoundFuncs = func() []string { return []string{"nativeProbe"} }
	p.InterceptResource("app", func(ResourceRequest, func(*ResourceResponse)) {})
	if err := p.Eval("void 0"); err == nil {
		t.Fatal("Eval before Run silently succeeded")
	}
	if err := p.Init(`window.preInit = 'installed';`); err != nil {
		t.Fatal(err)
	}
	html := `<!doctype html><html><body><script>webkit.messageHandlers.webviewBridge.postMessage('loaded:'+window.preInit+':'+typeof nativeProbe+':'+typeof window.lateInit)</script></body></html>`
	p.SetHTML(html)
	run := make(chan error, 1)
	go func() { run <- p.Run() }()
	defer func() { p.Close(); <-run }()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-messages:
			if got != want {
				t.Fatalf("message=%q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no JS message, want %q", want)
		}
	}
	expect("loaded:installed:function:undefined")
	if err := p.Eval(`window.evalValue='executed';webkit.messageHandlers.webviewBridge.postMessage('eval:'+window.evalValue)`); err != nil {
		t.Fatal(err)
	}
	expect("eval:executed")
	if err := p.Init(`window.lateInit='late';`); err != nil {
		t.Fatal(err)
	}
	if err := p.Eval(`webkit.messageHandlers.webviewBridge.postMessage('current:'+typeof window.lateInit)`); err != nil {
		t.Fatal(err)
	}
	expect("current:undefined")
	mainThread(func() {
		scripts := p.ucc.Send(objc.RegisterName("userScripts"))
		shim := fetchShimScript(p.schemeHandlers)
		found := false
		for i := 0; i < int(scripts.Send(objc.RegisterName("count"))); i++ {
			source := scripts.Send(objc.RegisterName("objectAtIndex:"), i).Send(objc.RegisterName("source"))
			if goString(source) == shim {
				found = true
			}
		}
		if !found {
			t.Error("runtime Init removed resource interception shim")
		}
	})
	// A URL navigation has no inline bootstrap shim to conceal missing user scripts.
	p.Navigate("data:text/html," + url.PathEscape(html))
	expect("loaded:installed:function:string")
}

func TestInitAndEvalAlertDispatch(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	alerts := make(chan string, 4)
	var original objc.Class
	mainThread(func() {
		original = nsAlertClass
		subclass, err := objc.RegisterClass(fmt.Sprintf("GoWebviewScriptAlert%d", time.Now().UnixNano()), nsAlertClass, nil, nil, []objc.MethodDef{{Cmd: objc.RegisterName("runModal"), Fn: func(id objc.ID, cmd objc.SEL) int64 {
			alerts <- goString(id.Send(objc.RegisterName("messageText")))
			return 1000
		}}})
		if err != nil {
			t.Error(err)
			return
		}
		nsAlertClass = subclass
	})
	defer mainThread(func() { nsAlertClass = original })
	loaded := make(chan struct{}, 1)
	p.MessageFunc = func(body string) {
		if body == "loaded" {
			loaded <- struct{}{}
		}
	}
	p.Init(`setTimeout(()=>alert('init alert'), 50)`)
	p.SetHTML(`<!doctype html><script>webkit.messageHandlers.webviewBridge.postMessage('loaded')</script>`)
	run := make(chan error, 1)
	go func() { run <- p.Run() }()
	defer func() { p.Close(); <-run }()
	select {
	case <-loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("page not loaded")
	}
	select {
	case got := <-alerts:
		if got != "init alert" {
			t.Fatal(got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Init alert not delivered to native delegate")
	}
	if err := p.Eval(`alert('eval alert')`); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-alerts:
		if got != "eval alert" {
			t.Fatal(got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Eval alert not delivered to native delegate")
	}
}

func TestInitPromiseSurvivesHTMLBootstrap(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	reports := make(chan string, 1)
	p.BoundFuncs = func() []string { return []string{"nativeProbe", "reportInit"} }
	p.MessageFunc = func(body string) {
		var message struct {
			ID   int      `json:"id"`
			Name string   `json:"name"`
			Args []string `json:"args"`
		}
		if err := json.Unmarshal([]byte(body), &message); err != nil {
			t.Error(err)
			return
		}
		if message.Name == "nativeProbe" {
			p.EvalHost(fmt.Sprintf("webviewBridge.resolve(%d,'ok')", message.ID))
		}
		if message.Name == "reportInit" && len(message.Args) > 0 {
			reports <- message.Args[0]
		}
	}
	if err := p.Init(`nativeProbe().then(value=>reportInit('init:'+value))`); err != nil {
		t.Fatal(err)
	}
	p.SetHTML(`<!doctype html><html><body>Init promise regression</body></html>`)
	run := make(chan error, 1)
	go func() { run <- p.Run() }()
	defer func() { p.Close(); <-run }()
	select {
	case got := <-reports:
		if got != "init:ok" {
			t.Fatal(got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("inline HTML bootstrap reset Init's pending promise")
	}
}
