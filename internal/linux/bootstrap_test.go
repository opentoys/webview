//go:build linux

package linux

import (
	"github.com/opentoys/webview/internal/bridgejs"
	"strings"
	"testing"
)

func TestBindingBootstrap(t *testing.T) {
	p := &gtk{BindingScript: func() string { return bridgejs.Proxies([]bridgejs.Binding{{Name: "app", Methods: []string{"Greet"}}}) }}
	src := p.bootstrapScript()
	for _, want := range []string{"window.webkit.messageHandlers.webviewBridge.postMessage", `\"name\":\"app\"`, `\"methods\":[\"Greet\"]`, "Object.defineProperty"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Index(src, "postMessage(msg)") > strings.Index(src, `\"name\":\"app\"`) {
		t.Fatal("proxies precede transport")
	}
}
