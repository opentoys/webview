package webview

import (
	"encoding/json"
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
	var got map[string]any
	json.Unmarshal([]byte(replies[0]), &got)
	if got["id"].(float64) != 1 || got["ok"] != true || got["result"].(float64) != 5 {
		t.Fatalf("bad reply: %v", got)
	}
}

func TestBridgeError(t *testing.T) {
	b := newBridge()
	b.Bind("boom", func() error { return errTest("nope") })
	msg := `{"id":2,"name":"boom","args":[]}`
	var replies []string
	b.HandleMessage(msg, func(js string) { replies = append(replies, js) })
	var got map[string]any
	json.Unmarshal([]byte(replies[0]), &got)
	if got["ok"] != false || got["error"] != "nope" {
		t.Fatalf("expected error reply, got %v", got)
	}
}

func TestBridgeUnknownFunc(t *testing.T) {
	b := newBridge()
	msg := `{"id":3,"name":"nope","args":[]}`
	var replies []string
	b.HandleMessage(msg, func(js string) { replies = append(replies, js) })
	var got map[string]any
	json.Unmarshal([]byte(replies[0]), &got)
	if got["ok"] != false {
		t.Fatalf("expected error reply, got %v", got)
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
	var got map[string]any
	if err := json.Unmarshal([]byte(replies[0]), &got); err != nil {
		t.Fatalf("reply is not JSON: %q", replies[0])
	}
	if got["ok"] != false || got["error"] == nil || got["error"] == "" {
		t.Fatalf("expected error reply, got %v", got)
	}
}