// Package bridgejs generates the transport-independent JavaScript binding layer.
package bridgejs

import "encoding/json"

type Binding struct {
	Name       string                     `json:"name"`
	Methods    []string                   `json:"methods"` // nil denotes a function; an empty slice denotes an object.
	Properties map[string]json.RawMessage `json:"properties,omitempty"`
}

func Functions(names []string) []Binding {
	out := make([]Binding, 0, len(names))
	for _, name := range names {
		out = append(out, Binding{Name: name})
	}
	return out
}

// Proxies replaces only framework-owned entries, independently of the transport
// and its pending promises. Define own properties to handle __proto__ safely.
func Proxies(bindings []Binding) string {
	if bindings == nil {
		bindings = []Binding{}
	}
	data, _ := json.Marshal(bindings)
	literal, _ := json.Marshal(string(data))
	return `(() => {
const key = Symbol.for("webview.bindingEntries");
const previous = window[key] || new Map();
for (const [name, value] of previous) {
 if (Object.prototype.hasOwnProperty.call(window, name) && window[name] === value) delete window[name];
}
const entries = new Map();
const freeze = value => {
 if (value !== null && typeof value === 'object') {
  for (const key of Object.keys(value)) freeze(value[key]);
  Object.freeze(value);
 }
 return value;
};
for (const binding of JSON.parse(` + string(literal) + `)) {
 const invoke = method => function(...args) {
  return window.webviewBridge.call(binding.name, args, method);
 };
 let value;
 if (binding.methods === null) value = invoke(undefined);
 else {
  value = {};
  for (const [name, snapshot] of Object.entries(binding.properties || {})) {
   Object.defineProperty(value, name, {value: freeze(snapshot), enumerable: true});
  }
  for (const method of binding.methods) {
   Object.defineProperty(value, method, {
    value: invoke(method), enumerable: true, configurable: true, writable: true
   });
  }
  Object.freeze(value);
 }
 Object.defineProperty(window, binding.name, {value, enumerable:true, configurable:true, writable:true});
 entries.set(binding.name, value);
}
window[key] = entries;
})();`
}

// Transport is idempotent: reinstallation must preserve outstanding promises.
func Transport(send string) string {
	return `if (!window.webviewBridge || typeof window.webviewBridge !== "object" || typeof window.webviewBridge.call !== "function") {
window.webviewBridge = {
 _pending: {},
 _next: 0,
 postMessage(msg) { ` + send + ` },
 resolve(id, result) {
  var p = this._pending[id];
  if (p) {
   p.resolve(result);
   delete this._pending[id];
  }
 },
 reject(id, err) {
  var p = this._pending[id];
  if (p) {
   p.reject(new Error(err));
   delete this._pending[id];
  }
 },
 call(name, args, method) {
  return new Promise((resolve, reject) => {
   var id = ++this._next;
   this._pending[id] = {resolve: resolve, reject: reject};
   try {
    this.postMessage({id: id, name: name, args: args, method: method});
   } catch (err) {
    delete this._pending[id];
    reject(err);
   }
  });
 }
};
}`
}
