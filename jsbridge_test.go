package webview

import (
	"strings"
	"testing"
)

func TestBridgeDispatch(t *testing.T) {
	b := newBridge()
	if err := b.Bind("add", func(a, b int) int { return a + b }); err != nil {
		t.Fatal(err)
	}
	msg := `{"id":1,"name":"add","args":[2,3]}`
	var replies []string
	b.HandleMessage(msg, func(js string) { replies = append(replies, js) })
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(replies))
	}
	if want := "webviewBridge.resolve(1, 5)"; replies[0] != want {
		t.Fatalf("reply = %q, want %q", replies[0], want)
	}
}

func TestBridgeError(t *testing.T) {
	b := newBridge()
	b.Bind("boom", func() error { return errTest("nope") })
	msg := `{"id":2,"name":"boom","args":[]}`
	var replies []string
	b.HandleMessage(msg, func(js string) { replies = append(replies, js) })
	if want := `webviewBridge.reject(2, "nope")`; replies[0] != want {
		t.Fatalf("reply = %q, want %q", replies[0], want)
	}
}

func TestBridgeUnknownFunc(t *testing.T) {
	b := newBridge()
	msg := `{"id":3,"name":"nope","args":[]}`
	var replies []string
	b.HandleMessage(msg, func(js string) { replies = append(replies, js) })
	if want := `webviewBridge.reject(3, "webview: no bound func \"nope\"")`; replies[0] != want {
		t.Fatalf("reply = %q, want %q", replies[0], want)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestBridgeUnencodableResult(t *testing.T) {
	b := newBridge()
	b.Bind("leak", func() chan int { return make(chan int) })
	msg := `{"id":4,"name":"leak","args":[]}`
	var replies []string
	b.HandleMessage(msg, func(js string) { replies = append(replies, js) })
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(replies))
	}
	if got := replies[0]; !strings.Contains(got, "webviewBridge.reject(4, ") || !strings.Contains(got, "json: unsupported type") {
		t.Fatalf("reply = %q, want reject with json error", got)
	}
}