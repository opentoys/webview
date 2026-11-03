package webview

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type bindingEmbedded struct{}

func (bindingEmbedded) Promoted() string { return "promoted" }

type bindingObject struct {
	bindingEmbedded
	Field string
}

func (bindingObject) Greet(name string) string { return "hi " + name }
func (*bindingObject) Pointer() string         { return "pointer" }
func (bindingObject) hidden() string           { return "hidden" }

type bindingOther struct{}

func (bindingOther) Other() int { return 7 }

func bridgeReply(b *bridge, name string, method *string, args any) string {
	payload := map[string]any{"id": 1, "name": name, "args": args}
	if method != nil {
		payload["method"] = *method
	}
	data, _ := json.Marshal(payload)
	var reply string
	b.HandleMessage(string(data), func(js string) { reply = js })
	return reply
}
func methodName(s string) *string { return &s }

func TestObjectMethodSets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		pointer bool
	}{
		{"value", bindingObject{}, false}, {"pointer", &bindingObject{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBridge()
			if err := b.Bind("app", tc.value); err != nil {
				t.Fatal(err)
			}
			if got := bridgeReply(b, "app", methodName("Greet"), []any{"张三"}); got != `webviewBridge.resolve(1, "hi 张三")` {
				t.Fatal(got)
			}
			if got := bridgeReply(b, "app", methodName("Promoted"), []any{}); got != `webviewBridge.resolve(1, "promoted")` {
				t.Fatal(got)
			}
			for _, name := range []string{"hidden", "Field", "Missing"} {
				if got := bridgeReply(b, "app", methodName(name), []any{}); !strings.Contains(got, "reject") {
					t.Fatal(got)
				}
			}
			got := bridgeReply(b, "app", methodName("Pointer"), []any{})
			if strings.Contains(got, "resolve") != tc.pointer {
				t.Fatal(got)
			}
			if got := bridgeReply(b, "app", nil, []any{}); !strings.Contains(got, "reject") {
				t.Fatal(got)
			}
		})
	}
}

func TestBindingReplacementAndInvalidValues(t *testing.T) {
	b := newBridge()
	var nilFunc func()
	var nilObject *bindingObject
	n := 1
	for _, value := range []any{nil, nilFunc, nilObject, 1, "str", &n, []int{}, map[string]int{}, struct{}{}, new(struct{})} {
		if err := b.Bind("app", bindingObject{}); err != nil {
			t.Fatal(err)
		}
		if err := b.Bind("app", value); err == nil {
			t.Fatalf("accepted %T", value)
		}
		if got := bridgeReply(b, "app", methodName("Greet"), []any{"world"}); !strings.Contains(got, `"hi world"`) {
			t.Fatalf("invalid replacement changed binding: %s", got)
		}
	}
	if err := b.Bind("app", bindingOther{}); err != nil {
		t.Fatal(err)
	}
	if got := bridgeReply(b, "app", methodName("Greet"), []any{"world"}); !strings.Contains(got, "reject") {
		t.Fatal(got)
	}
	if got := bridgeReply(b, "app", methodName("Other"), []any{}); got != "webviewBridge.resolve(1, 7)" {
		t.Fatal(got)
	}
	b.Bind("app", func() string { return "function" })
	if got := bridgeReply(b, "app", nil, []any{}); got != `webviewBridge.resolve(1, "function")` {
		t.Fatal(got)
	}
	if got := bridgeReply(b, "app", methodName("Other"), []any{}); !strings.Contains(got, "reject") {
		t.Fatal(got)
	}
	b.Bind("app", bindingObject{})
	b.Bind("app.Greet", func() string { return "literal" })
	if got := bridgeReply(b, "app.Greet", nil, []any{}); got != `webviewBridge.resolve(1, "literal")` {
		t.Fatal(got)
	}
	b.Unbind("app")
	if got := bridgeReply(b, "app", methodName("Greet"), []any{"world"}); !strings.Contains(got, "reject") {
		t.Fatal(got)
	}
	if b.Unbind("unknown") {
		t.Fatal("unknown unbind changed registry")
	}
}

func TestCallDuringUnbindAndConcurrentReplacement(t *testing.T) {
	b := newBridge()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan string, 1)
	b.Bind("slow", func() string { close(entered); <-release; return "finished" })
	go func() { done <- bridgeReply(b, "slow", nil, []any{}) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("call did not start")
	}
	b.Unbind("slow")
	if got := bridgeReply(b, "slow", nil, []any{}); !strings.Contains(got, "reject") {
		t.Fatal(got)
	}
	close(release)
	if got := <-done; got != `webviewBridge.resolve(1, "finished")` {
		t.Fatal(got)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b.Bind("app", bindingObject{})
				bridgeReply(b, "app", methodName("Greet"), []any{"world"})
				b.bindingScript()
				b.Unbind("app")
			}
		}()
	}
	wg.Wait()
}

func TestFunctionConversionsAndPanic(t *testing.T) {
	b := newBridge()
	type payload struct {
		Value int `json:"value"`
	}
	b.Bind("convert", func(p payload) (payload, error) { p.Value++; return p, nil })
	if got := bridgeReply(b, "convert", nil, []any{map[string]any{"value": 3}}); got != `webviewBridge.resolve(1, {"value":4})` {
		t.Fatal(got)
	}
	b.Bind("panic", func() { panic("boom") })
	if got := bridgeReply(b, "panic", nil, []any{}); !strings.Contains(got, "panic: boom") {
		t.Fatal(got)
	}
}

func TestBridgeCallsRunOutsideRegistryLock(t *testing.T) {
	b := newBridge()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	b.Bind("parallel", func() { entered <- struct{}{}; <-release })
	for i := 0; i < 2; i++ {
		go bridgeReply(b, "parallel", nil, []any{})
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("calls were serialized or registry lock held during user code")
		}
	}
	b.Unbind("parallel")
}

type bindingPlatform struct {
	dialogPlatformStub
	refreshes int
}

func (p *bindingPlatform) RefreshBindings() error { p.refreshes++; return nil }

func TestWindowBindingRefresh(t *testing.T) {
	p := &bindingPlatform{}
	w := &W{p: p, bridge: newBridge()}
	if err := w.Bind("app", bindingObject{}); err != nil {
		t.Fatal(err)
	}
	if err := w.Bind("app", nil); err == nil {
		t.Fatal("accepted nil")
	}
	if err := w.Unbind("unknown"); err != nil {
		t.Fatal(err)
	}
	if p.refreshes != 1 {
		t.Fatalf("refreshes=%d", p.refreshes)
	}
	if err := w.Bind("app", func() {}); err != nil {
		t.Fatal(err)
	}
	if err := w.Unbind("app"); err != nil {
		t.Fatal(err)
	}
	if p.refreshes != 3 {
		t.Fatalf("refreshes=%d", p.refreshes)
	}
}
