package webview

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"sync"

	"github.com/opentoys/webview/internal/bridgejs"
)

type bridge struct {
	closed bool
	mu     sync.Mutex
	funcs  map[string]binding
}

func newBridge() *bridge {
	return &bridge{funcs: map[string]binding{}}
}

type binding struct {
	fn         any
	methods    map[string]any
	properties map[string]json.RawMessage
}

func (b *bridge) Bind(name string, value any) error {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return fmt.Errorf("webview: nil binding for %q", name)
	}
	next := binding{}
	switch v.Kind() {
	case reflect.Func:
		if v.IsNil() {
			return fmt.Errorf("webview: nil func for %q", name)
		}
		next.fn = value
	case reflect.Pointer, reflect.Struct:
		if v.Kind() == reflect.Pointer && (v.IsNil() || v.Type().Elem().Kind() != reflect.Struct) {
			return fmt.Errorf("webview: %q requires a non-nil *struct", name)
		}
		next.methods = make(map[string]any)
		for i := 0; i < v.NumMethod(); i++ {
			method := v.Type().Method(i)
			if method.PkgPath == "" {
				next.methods[method.Name] = v.Method(i).Interface()
			}
		}
		fields := v
		if fields.Kind() == reflect.Pointer {
			fields = fields.Elem()
		}
		next.properties = make(map[string]json.RawMessage)
		for _, field := range reflect.VisibleFields(fields.Type()) {
			if field.PkgPath != "" {
				continue
			}
			fv, err := fields.FieldByIndexErr(field.Index)
			if err != nil {
				continue
			} // Promoted field behind a nil embedded pointer.
			if _, exists := next.methods[field.Name]; exists {
				return fmt.Errorf("webview: field and method %q conflict on %q", field.Name, name)
			}
			data, err := json.Marshal(fv.Interface())
			if err != nil {
				return fmt.Errorf("webview: field %q on %q: %w", field.Name, name, err)
			}
			next.properties[field.Name] = data
		}
		if len(next.methods) == 0 && len(next.properties) == 0 {
			return fmt.Errorf("webview: %q has no exported fields or methods", name)
		}
	case reflect.Map:
		values, ok := value.(map[string]any)
		if !ok || values == nil {
			return fmt.Errorf("webview: %q requires a non-nil map[string]any", name)
		}
		next.methods = make(map[string]any)
		next.properties = make(map[string]json.RawMessage)
		for key, member := range values {
			mv := reflect.ValueOf(member)
			if mv.IsValid() && mv.Kind() == reflect.Func {
				if mv.IsNil() {
					return fmt.Errorf("webview: nil func %q on %q", key, name)
				}
				next.methods[key] = member
			} else {
				data, err := json.Marshal(member)
				if err != nil {
					return fmt.Errorf("webview: property %q on %q: %w", key, name, err)
				}
				next.properties[key] = data
			}
		}
	default:
		return fmt.Errorf("webview: %q requires a func, struct, *struct, or map[string]any", name)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.funcs[name] = next
	return nil
}

func (b *bridge) bindingScript() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	bindings := make([]bridgejs.Binding, 0, len(b.funcs))
	for name, entry := range b.funcs {
		description := bridgejs.Binding{Name: name, Properties: entry.properties}
		if entry.methods != nil {
			description.Methods = make([]string, 0, len(entry.methods))
			for method := range entry.methods {
				description.Methods = append(description.Methods, method)
			}
			sort.Strings(description.Methods)
		}
		bindings = append(bindings, description)
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Name < bindings[j].Name })
	return bridgejs.Proxies(bindings)
}

func (b *bridge) Unbind(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, exists := b.funcs[name]
	delete(b.funcs, name)
	return exists
}

// HandleMessage processes a JS -> Go {id,name,args,method?} message and emits reply JS.
func (b *bridge) HandleMessage(msg string, emit func(string)) {
	var in struct {
		ID     int             `json:"id"`
		Name   string          `json:"name"`
		Method *string         `json:"method"`
		Args   json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(msg), &in); err != nil {
		emit(renderError(0, err))
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	entry, ok := b.funcs[in.Name]
	var fn any
	if ok {
		if in.Method == nil {
			fn = entry.fn
		} else {
			fn = entry.methods[*in.Method]
		}
	}
	b.mu.Unlock()
	if !ok || fn == nil {
		if in.Method != nil {
			emit(renderError(in.ID, fmt.Errorf("webview: no bound method %q on %q", *in.Method, in.Name)))
		} else {
			emit(renderError(in.ID, fmt.Errorf("webview: no bound func %q", in.Name)))
		}
		return
	}
	result, err := callFunc(fn, in.Args)
	if err != nil {
		emit(renderError(in.ID, err))
		return
	}
	payload, err := json.Marshal(result)
	if err != nil {
		emit(renderError(in.ID, err))
		return
	}
	emit("webviewBridge.resolve(" + strconv.Itoa(in.ID) + ", " + string(payload) + ")")
}

func renderError(id int, err error) string {
	lit, jerr := json.Marshal(err.Error())
	if jerr != nil {
		return "webviewBridge.reject(null)"
	}
	return "webviewBridge.reject(" + strconv.Itoa(id) + ", " + string(lit) + ")"
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
func (b *bridge) Close() { b.mu.Lock(); b.closed = true; clear(b.funcs); b.mu.Unlock() }

// Keep the internal file-input capture ordered with native chooser requests;
// application bindings always run outside native callback stacks.
func (w *W) dispatchMessage(body string, emit func(string)) {
	var message struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal([]byte(body), &message)
	if message.Name == "__accept__" {
		w.bridge.HandleMessage(body, emit)
		return
	}
	go func() {
		if !app.quitting.Load() {
			w.bridge.HandleMessage(body, emit)
		}
	}()
}
