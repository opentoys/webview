//go:build windows

package windows

import (
	"strings"
	"testing"

	"github.com/opentoys/webview/internal/bridgejs"
)

func TestBootstrapJS(t *testing.T) {
	p := &Platform{BindingScript: func() string { return bridgejs.Proxies(bridgejs.Functions([]string{"greet", "add"})) }}
	js := p.bootstrapScript()
	if !strings.Contains(js, "window.chrome.webview.postMessage") {
		t.Fatal("missing chrome.webview.postMessage")
	}
	if !strings.Contains(js, `\"name\":\"greet\"`) {
		t.Fatal("missing greet stub")
	}
	if !strings.Contains(js, `\"name\":\"add\"`) {
		t.Fatal("missing add stub")
	}
	if !strings.Contains(js, "webviewBridge") {
		t.Fatal("missing webviewBridge")
	}
}

func TestBootstrapJSEmpty(t *testing.T) {
	js := bootstrapJS()
	if !strings.Contains(js, "window.webviewBridge") {
		t.Fatal("missing webviewBridge")
	}
	if strings.Contains(js, "window[undefined]") {
		t.Fatal("should not contain undefined stubs")
	}
}

func TestBindingBootstrapCallback(t *testing.T) {
	p := &Platform{BindingScript: func() string { return "/*current-object-methods*/" }}
	src := p.bootstrapScript()
	if !strings.HasSuffix(src, "/*current-object-methods*/") {
		t.Fatal("missing current bindings")
	}
	if !strings.Contains(src, "window.chrome.webview.postMessage") {
		t.Fatal("missing transport")
	}
}
