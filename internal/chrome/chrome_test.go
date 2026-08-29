package chrome

import (
	"strings"
	"testing"
)

func TestBootstrapJSRegistersBindingTransport(t *testing.T) {
	s := bootstrapJS()
	for _, want := range []string{
		"window.webviewBridge",
		"this.postMessage",
		"_pending",
		"resolve(id, result)",
		"reject(id, err)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bootstrapJS missing %q\n%s", want, s)
		}
	}
	// transport must call the native CDP binding (a plain function call).
	if !strings.Contains(s, "transport(") {
		t.Errorf("bootstrapJS must call the native binding fn; got:\n%s", s)
	}
}

func TestStubJSPerName(t *testing.T) {
	s := stubJS([]string{"foo", "bar"})
	if !strings.Contains(s, `window["foo"]`) || !strings.Contains(s, `window["bar"]`) {
		t.Errorf("stubJS missing name stubs:\n%s", s)
	}
	if !strings.Contains(s, "webviewBridge.call") {
		t.Errorf("stubJS must call webviewBridge.call:\n%s", s)
	}
}
