//go:build darwin

package darwin

import (
	"sync"
	"testing"
	"time"
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
