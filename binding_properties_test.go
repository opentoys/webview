package webview

import (
	"encoding/json"
	"strings"
	"testing"
)

type propertyEmbedded struct{ Promoted string }
type propertyObject struct {
	propertyEmbedded
	Name   string `json:"renamed"`
	Nested map[string]any
	hidden string
}

func (*propertyObject) Hello() string { return "hello" }

func TestReadOnlyPropertySnapshots(t *testing.T) {
	b := newBridge()
	obj := &propertyObject{propertyEmbedded: propertyEmbedded{Promoted: "visible"}, Name: "first", Nested: map[string]any{"n": 1}, hidden: "secret"}
	if err := b.Bind("app", obj); err != nil {
		t.Fatal(err)
	}
	obj.Name = "changed"
	obj.Nested["n"] = 2
	properties := b.funcs["app"].properties
	for key, want := range map[string]string{"Name": `"first"`, "Promoted": `"visible"`, "Nested": `{"n":1}`} {
		if got := string(properties[key]); got != want {
			t.Fatalf("%s=%s, want %s", key, got, want)
		}
	}
	if _, ok := properties["hidden"]; ok {
		t.Fatal("unexported field exposed")
	}
	if _, ok := properties["renamed"]; ok {
		t.Fatal("JSON tag renamed Go field")
	}
	if err := b.Bind("app", obj); err != nil {
		t.Fatal(err)
	}
	if string(b.funcs["app"].properties["Name"]) != `"changed"` {
		t.Fatal("rebind did not refresh snapshot")
	}
	if got := bridgeReply(b, "app", methodName("Hello"), []any{}); got != `webviewBridge.resolve(1, "hello")` {
		t.Fatal(got)
	}
}

func TestMapBindings(t *testing.T) {
	b := newBridge()
	values := map[string]any{"version": "1.0", "nil": nil, "nested": map[string]any{"__proto__": "safe"}, "greet": func(name string) string { return "hi " + name }}
	if err := b.Bind("app", values); err != nil {
		t.Fatal(err)
	}
	values["version"] = "2.0"
	values["nested"].(map[string]any)["__proto__"] = "changed"
	delete(values, "greet")
	if got := string(b.funcs["app"].properties["version"]); got != `"1.0"` {
		t.Fatal(got)
	}
	if got := string(b.funcs["app"].properties["nested"]); got != `{"__proto__":"safe"}` {
		t.Fatal(got)
	}
	if got := bridgeReply(b, "app", methodName("greet"), []any{"map"}); got != `webviewBridge.resolve(1, "hi map")` {
		t.Fatal(got)
	}
	if got := bridgeReply(b, "app", methodName("version"), []any{}); !strings.HasPrefix(got, "webviewBridge.reject") {
		t.Fatal(got)
	}
	if err := b.Bind("app", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if b.funcs["app"].methods == nil {
		t.Fatal("empty map treated as function")
	}
	var nilMap map[string]any
	var nilFn func()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	for _, invalid := range []any{nilMap, map[string]any{"fn": nilFn}, map[string]any{"bad": make(chan int)}, cyclic, struct{ Bad chan int }{make(chan int)}} {
		if err := b.Bind("app", invalid); err == nil {
			t.Fatalf("accepted %T", invalid)
		}
		if len(b.funcs["app"].properties) != 0 {
			t.Fatal("failed replacement changed binding")
		}
	}
	if err := b.Bind("data", struct{ Value int }{42}); err != nil {
		t.Fatal(err)
	}
	if string(b.funcs["data"].properties["Value"]) != "42" {
		t.Fatal("field-only struct unsupported")
	}
}

func TestNilEmbeddedPropertyPointer(t *testing.T) {
	type embedded struct {
		*propertyEmbedded
		Own int
	}
	b := newBridge()
	if err := b.Bind("app", embedded{Own: 1}); err != nil {
		t.Fatal(err)
	}
	if _, exists := b.funcs["app"].properties["Promoted"]; exists {
		t.Fatal("nil promoted field exposed")
	}
	if !json.Valid(b.funcs["app"].properties["Own"]) {
		t.Fatal("invalid property JSON")
	}
}
