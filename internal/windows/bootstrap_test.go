//go:build windows

package windows

import (
	"strings"
	"testing"
)

func TestBootstrapJS(t *testing.T) {
	js := bootstrapJS([]string{"greet", "add"})
	if !strings.Contains(js, "window.chrome.webview.postMessage") {
		t.Fatal("missing chrome.webview.postMessage")
	}
	if !strings.Contains(js, "window[\"greet\"]") {
		t.Fatal("missing greet stub")
	}
	if !strings.Contains(js, "window[\"add\"]") {
		t.Fatal("missing add stub")
	}
	if !strings.Contains(js, "webviewBridge") {
		t.Fatal("missing webviewBridge")
	}
}

func TestBootstrapJSEmpty(t *testing.T) {
	js := bootstrapJS(nil)
	if !strings.Contains(js, "window.webviewBridge") {
		t.Fatal("missing webviewBridge")
	}
	if strings.Contains(js, "window[undefined]") {
		t.Fatal("should not contain undefined stubs")
	}
}
