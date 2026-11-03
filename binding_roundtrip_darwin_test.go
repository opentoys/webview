//go:build darwin

package webview

import (
	"io"
	"testing"

	"github.com/opentoys/webview/internal/darwin"
	"github.com/opentoys/webview/internal/debuglog"
)

func TestObjectRoundtripDarwin(t *testing.T) {
	b := newBridge()
	p, err := darwin.New()
	if err != nil {
		t.Fatal(err)
	}
	p.Logger = debuglog.New(io.Discard)
	p.BindingScript = b.bindingScript
	p.MessageFunc = func(body string) { go b.HandleMessage(body, p.EvalHost) }
	exerciseObjectBackend(t, p, b)
}
