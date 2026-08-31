package chrome

import (
	"encoding/json"
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

func TestMainTargetLifecycleFiltering(t *testing.T) {
	c := &Chrome{sessionID: "main-session", targetID: "main-target"}
	tests := []struct {
		name   string
		method string
		params string
		want   bool
	}{
		{"main target detached", "Target.detachedFromTarget", `{"sessionId":"main-session","targetId":"main-target"}`, true},
		{"transient target detached", "Target.detachedFromTarget", `{"sessionId":"other-session","targetId":"other-target"}`, false},
		{"main target destroyed", "Target.targetDestroyed", `{"targetId":"main-target"}`, true},
		{"worker destroyed", "Target.targetDestroyed", `{"targetId":"worker-target"}`, false},
		{"malformed event", "Target.targetDestroyed", `{`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.isMainTargetClosed(tt.method, json.RawMessage(tt.params)); got != tt.want {
				t.Fatalf("isMainTargetClosed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFirstPageTargetRemainsMainTarget(t *testing.T) {
	c := &Chrome{attached: make(chan struct{})}
	c.dispatch(cdpMsg{
		Method: "Target.attachedToTarget",
		Params: json.RawMessage(`{"sessionId":"first-session","targetInfo":{"targetId":"first-target","type":"page"}}`),
	})
	c.dispatch(cdpMsg{
		Method: "Target.attachedToTarget",
		Params: json.RawMessage(`{"sessionId":"popup-session","targetInfo":{"targetId":"popup-target","type":"page"}}`),
	})

	if c.sessionID != "first-session" || c.targetID != "first-target" {
		t.Fatalf("main target changed to session=%q target=%q", c.sessionID, c.targetID)
	}
}
