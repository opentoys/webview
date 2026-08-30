//go:build darwin

package darwin

import (
	"strings"
	"testing"
	"time"
)

// TestNavigateBeforeRun: a URL passed to Navigate before Run() must reach the
// live webview. White screen = the call was silently dropped. Verify via JS:
// evaluate location.href back through the existing message bridge (postMessage
// to webviewBridge) and check the page actually loaded the target origin.

func TestNavigateBeforeRun(t *testing.T) {
	p, _ := New()
	target := "https://www.baidu.com"
	loc := make(chan string, 1)
	p.MessageFunc = func(body string) {
		if strings.HasPrefix(body, "loc=") {
			select {
			case loc <- strings.TrimPrefix(body, "loc="):
			default:
			}
		}
	}
	// Navigate BEFORE Run: without pending support this is silently dropped.
	if err := p.Navigate(target); err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	defer func() {
		p.Close()
		<-errCh
	}()

	// Poll location.href, re-eval'ing each tick so we don't race the load.
	deadline := time.Now().Add(15 * time.Second)
	msg := `webkit.messageHandlers.webviewBridge.postMessage('loc=' + location.href)`
	for {
		p.EvalHost(msg)
		select {
		case href := <-loc:
			if strings.Contains(href, "baidu.com") {
				return // expected: page navigated to target
			}
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: webview never loaded %s (pre-Run Navigate likely dropped)", target)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
