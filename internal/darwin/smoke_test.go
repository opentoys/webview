//go:build darwin

package darwin

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

func TestWindowOpens(t *testing.T) {
	p, _ := New()
	done := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.Close()
		done <- struct{}{}
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	<-done
}

func TestTitleAndHTML(t *testing.T) {
	p, _ := New()
	p.SetTitle("hello")
	p.SetHTML("<html><body><h1 id='t'>old</h1></body></html>")
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.Close()
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
}

// TestHTMLBeforeRun verifies SetHTML before Run() is not dropped: previously
// it was a silent no-op (window showed white), because the webview doesn't
// exist until Run() creates it.
func TestHTMLBeforeRun(t *testing.T) {
	p, _ := New()
	var mu sync.Mutex
	var got string
	const want = "pre-run-html"
	p.MessageFunc = func(body string) {
		mu.Lock()
		got = body
		mu.Unlock()
	}
	// SetHTML BEFORE Run: the page script reports back via postMessage once it
	// actually loads.
	p.SetHTML(`<html><body><script>
		window.webkit.messageHandlers.webviewBridge.postMessage('pre-run-html');
	</script></body></html>`)
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	defer func() {
		p.Close()
		<-errCh
	}()
	deadline := time.Now().Add(4 * time.Second)
	for {
		mu.Lock()
		ok := got == want
		mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out: pre-Run SetHTML never loaded; got %q", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestEval(t *testing.T) {
	p, _ := New()
	p.SetHTML("<html><body><h1 id='t'>old</h1></body></html>")
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.Close()
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
}

func TestScriptMessage(t *testing.T) {
	var mu sync.Mutex
	var got string
	var want = "hello-from-js"
	p, _ := New()
	p.MessageFunc = func(body string) {
		mu.Lock()
		got = body
		mu.Unlock()
	}
	// SetTitle carries through before Run; HTML does not (Navigate is a no-op
	// until the webview exists), so load it once Run has started.
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	time.Sleep(300 * time.Millisecond)
	p.SetHTML(`<html><body><script>
		window.webkit.messageHandlers.webviewBridge.postMessage('hello-from-js');
	</script></body></html>`)
	// Poll for the message from the JS side; WKWebView page load is async.
	deadline := time.Now().Add(4 * time.Second)
	for {
		mu.Lock()
		ok := got == want
		mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			p.Close()
			<-errCh
			mu.Lock()
			defer mu.Unlock()
			t.Fatalf("timed out, got %q, want %q", got, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.Close()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() = %v", err)
	}
}

// TestCustomSchemeSupportsOPFS verifies that a document served through a
// WKURLSchemeHandler has both a secure context and an origin-backed OPFS. This
// must be exercised in WebKit rather than faked by overwriting isSecureContext.
func TestCustomSchemeSupportsOPFS(t *testing.T) {
	p, _ := New()
	got := make(chan string, 1)
	p.MessageFunc = func(body string) { got <- body }
	p.InterceptResource("webviewtest", func(_ ResourceRequest, respond func(*ResourceResponse)) {
		respond(&ResourceResponse{
			Headers: http.Header{"Content-Type": {"text/html"}},
			Body: []byte(`<script>
				(async () => {
					try {
						await navigator.storage.getDirectory();
						window.webkit.messageHandlers.webviewBridge.postMessage(String(window.isSecureContext) + ':opfs-ok');
					} catch (error) {
						window.webkit.messageHandlers.webviewBridge.postMessage(String(window.isSecureContext) + ':' + error.name);
					}
				})();
			</script>`),
		})
	})
	if err := p.Navigate("webviewtest://host/"); err != nil {
		t.Fatalf("Navigate() = %v", err)
	}

	runErr := make(chan error, 1)
	go func() { runErr <- p.Run() }()
	defer func() {
		p.Close()
		if err := <-runErr; err != nil {
			t.Errorf("Run() = %v", err)
		}
	}()

	select {
	case secure := <-got:
		if secure != "true:opfs-ok" {
			t.Fatalf("custom-scheme OPFS result = %q, want true:opfs-ok", secure)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for custom-scheme page")
	}
}

// TestRunReturnsOnWindowClose: closing the window (as the user would) must make
// Run() return instead of hanging.
func TestRunReturnsOnWindowClose(t *testing.T) {
	p, _ := New()
	runErr := make(chan error, 1)
	go func() { runErr <- p.Run() }()
	// Wait until the window exists, then close it the way the user does:
	// performClose: triggers the titlebar close button path (windowShouldClose:
	// + windowWillClose:).
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.mu.Lock()
		w := p.window
		p.mu.Unlock()
		if w != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("window never created")
		}
		time.Sleep(50 * time.Millisecond)
	}
	mainThread(func() {
		p.mu.Lock()
		w := p.window
		p.mu.Unlock()
		objc.ID(w).Send(objc.RegisterName("performClose:"), 0)
	})
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after window closed")
	}
}

// TestEditMenuInstalled: Cmd-C/Cmd-V need an Edit menu (key equivalents route
// via the main menu). Menus go through SetMenus -> applyMenus (the shared
// entry point for all platforms), so wire a default-style Edit menu and verify
// the Edit submenu renders with a Cmd-C key equivalent.
func TestEditMenuInstalled(t *testing.T) {
	p, _ := New()
	// Menus flow through SetMenus -> applyMenus (the shared entry point for
	// all platforms), so wire a default-style Edit menu explicitly.
	p.SetMenus([]Menu{
		{Label: "Edit", Items: []MenuItem{
			{Label: "Copy", Shortcut: "Cmd+C"},
		}},
	})
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	time.Sleep(500 * time.Millisecond)
	p.mu.Lock()
	w := p.window
	p.mu.Unlock()
	if w == 0 {
		t.Fatal("window not created")
	}
	var ok bool
	mainThread(func() {
		// The shared app's main menu has an Edit submenu whose items carry a
		// Cmd key equivalent. Find the Edit item with keyEquivalent "c".
		app := objc.ID(nsAppClass).Send(sharedApplicationSel)
		mainMenu := app.Send(objc.RegisterName("mainMenu"))
		if mainMenu == 0 {
			return
		}
		items := mainMenu.Send(objc.RegisterName("itemArray"))
		if items == 0 {
			return
		}
		n := objc.ID(items).Send(objc.RegisterName("count"))
		for i := 0; i < int(n); i++ {
			item := objc.ID(items).Send(objc.RegisterName("objectAtIndex:"), i)
			title := goString(objc.ID(item).Send(objc.RegisterName("title")))
			if title != "Edit" {
				continue
			}
			sub := objc.ID(item).Send(objc.RegisterName("submenu"))
			if sub == 0 {
				return
			}
			subItems := objc.ID(sub).Send(objc.RegisterName("itemArray"))
			m := objc.ID(subItems).Send(objc.RegisterName("count"))
			for j := 0; j < int(m); j++ {
				si := objc.ID(subItems).Send(objc.RegisterName("objectAtIndex:"), j)
				key := goString(objc.ID(si).Send(objc.RegisterName("keyEquivalent")))
				if key == "c" {
					ok = true
				}
			}
		}
	})
	p.Close()
	<-errCh
	if !ok {
		t.Fatal("Edit menu: no item with Cmd-C key equivalent")
	}
}

// TestIncognito: the webview config must use a non-persistent (in-memory)
// data store. WKWebsiteDataStore exposes isPersistent; non-persistent stores
// report false.
func TestIncognito(t *testing.T) {
	p, _ := New()
	p.Incognito = true
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	time.Sleep(500 * time.Millisecond)
	p.mu.Lock()
	wv := p.webview
	p.mu.Unlock()
	if wv == 0 {
		t.Fatal("webview not created")
	}
	var persistent bool
	mainThread(func() {
		cfg := objc.ID(wv).Send(objc.RegisterName("configuration"))
		store := objc.ID(cfg).Send(objc.RegisterName("websiteDataStore"))
		persistent = objc.ID(store).Send(objc.RegisterName("isPersistent")) != 0
	})
	p.Close()
	<-errCh
	if persistent {
		t.Fatal("incognito webview used a persistent data store")
	}
}

func TestOpenPanelClassCached(t *testing.T) {
	if wkOpenPanelParamsClass == 0 || allowsMultipleSelectionSel == 0 {
		t.Fatal("WKOpenPanelParameters class / allowsMultipleSelection selector not cached")
	}
}
