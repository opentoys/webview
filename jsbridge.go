package webview

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

type bridge struct {
	mu    sync.Mutex
	funcs map[string]any
}

func newBridge() *bridge {
	return &bridge{funcs: map[string]any{}}
}

func (b *bridge) Bind(name string, fn any) error {
	if fn == nil {
		return fmt.Errorf("webview: nil func for %q", name)
	}
	typ := reflect.TypeOf(fn)
	if typ.Kind() != reflect.Func {
		return fmt.Errorf("webview: %q not a func", name)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.funcs[name] = fn
	return nil
}

// HandleMessage processes a JS -> Go {id,name,args} message and emits reply JS.
func (b *bridge) HandleMessage(msg string, emit func(string)) {
	var in struct {
		ID   int             `json:"id"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(msg), &in); err != nil {
		emit(renderError(0, err))
		return
	}
	b.mu.Lock()
	fn, ok := b.funcs[in.Name]
	b.mu.Unlock()
	if !ok {
		emit(renderError(in.ID, fmt.Errorf("webview: no bound func %q", in.Name)))
		return
	}
	result, err := callFunc(fn, in.Args)
	if err != nil {
		emit(renderError(in.ID, err))
		return
	}
	payload, _ := json.Marshal(map[string]any{"id": in.ID, "ok": true, "result": result})
	emit(string(payload))
}

func renderError(id int, err error) string {
	payload, _ := json.Marshal(map[string]any{"id": id, "ok": false, "error": err.Error()})
	return string(payload)
}

// callFunc calls fn with JSON args, returns JSON-encodable result.
func callFunc(fn any, raw json.RawMessage) (result any, err error) {
	typ := reflect.TypeOf(fn)
	val := reflect.ValueOf(fn)

	var in []any
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if len(in) != typ.NumIn() {
		return nil, fmt.Errorf("webview: want %d args got %d", typ.NumIn(), len(in))
	}
	callArgs := make([]reflect.Value, len(in))
	for i, a := range in {
		t := typ.In(i)
		v := reflect.New(t)
		buf, _ := json.Marshal(a)
		if err := json.Unmarshal(buf, v.Interface()); err != nil {
			return nil, err
		}
		callArgs[i] = v.Elem()
	}

	var out []reflect.Value
	func() {
		defer func() {
			if r := recover(); r != nil {
				out = []reflect.Value{reflect.ValueOf(fmt.Errorf("webview: panic: %v", r))}
			}
		}()
		out = val.Call(callArgs)
	}()

	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		if err, ok := out[0].Interface().(error); ok && err != nil {
			return nil, err
		}
		return out[0].Interface(), nil
	case 2:
		ret := out[0].Interface()
		if err, ok := out[1].Interface().(error); ok && err != nil {
			return nil, err
		}
		return ret, nil
	default:
		return nil, fmt.Errorf("webview: too many return values")
	}
}