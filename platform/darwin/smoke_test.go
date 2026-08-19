//go:build darwin

package darwin

import (
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

func TestWindowOpens(t *testing.T) {
	p := New()
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
	p := New()
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
	p := New()
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
	p := New()
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
	p := New()
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

// TestRunReturnsOnWindowClose: closing the window (as the user would) must make
// Run() return instead of hanging.
func TestRunReturnsOnWindowClose(t *testing.T) {
	p := New()
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
// via the main menu). A bare AppKit app without a nib has no menu, so verify
// setupMainMenu installed one.
func TestEditMenuInstalled(t *testing.T) {
	p := New()
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
		// The shared app's main menu has an Edit submenu whose items map Cmd
		// key equivalents to cut:/copy:/paste:/selectAll: actions. Find the
		// item with keyEquivalent "c" and action copy:.
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
				action := objc.ID(si).Send(objc.RegisterName("action"))
				if key == "c" && uintptr(action) == uintptr(objc.RegisterName("copy:")) {
					ok = true
				}
			}
		}
	})
	p.Close()
	<-errCh
	if !ok {
		t.Fatal("Edit menu: no item mapping Cmd-C to copy:")
	}
}
