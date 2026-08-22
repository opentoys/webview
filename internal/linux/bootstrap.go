//go:build linux

package linux

import "encoding/json"

// bootstrapJS builds the bridge bootstrap script: window.webviewBridge with
// postMessage/resolve/reject/call plus a window.<name> stub per bound func.
// Uses window.webkit.messageHandlers.webviewBridge.postMessage (WebKitGTK
// script message handler), same transport as the darwin WKWebView backend.
func bootstrapJS(names []string) string {
	s := `window.webviewBridge = {
  _pending: {}, _next: 0,
  postMessage(msg) { window.webkit.messageHandlers.webviewBridge.postMessage(JSON.stringify(msg)); },
  resolve(id, result) { var p = this._pending[id]; if (p) { p.resolve(result); delete this._pending[id]; } },
  reject(id, err) { var p = this._pending[id]; if (p) { p.reject(new Error(err)); delete this._pending[id]; } },
  call(name, args) { return new Promise((resolve, reject) => {
    var id = ++this._next;
    this._pending[id] = {resolve: resolve, reject: reject};
    this.postMessage({id: id, name: name, args: args});
  }); }
};`
	for _, name := range names {
		lit, _ := json.Marshal(name)
		s += "window[" + string(lit) + "] = function() { return window.webviewBridge.call(" + string(lit) + ", Array.prototype.slice.call(arguments)); };"
	}
	return s
}
