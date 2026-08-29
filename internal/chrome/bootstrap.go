package chrome

import "encoding/json"

// bootstrapJS builds the bridge transport object. CDP Runtime.addBinding
// exposes a global function window.webviewBridge (the native binding). We
// capture it and replace window.webviewBridge with an object that wraps it,
// keeping the shared webview protocol: postMessage(JSON.stringify({id,name,args}))
// with resolve/reject completing from Go replies.
//
// Registered via Page.addScriptToEvaluateOnNewDocument so it runs on every
// document. The page context may be created before Runtime.addBinding is sent
// (content is baked into --app), so the installer polls until the native
// binding global appears rather than capturing it once.
func bootstrapJS() string {
	return `(() => {
  if (window.__wvInstalled) return;
  function setup() {
    var transport = window.webviewBridge;
    if (typeof transport !== 'function') return; // binding not ready yet
    window.__wvInstalled = true;
    window.webviewBridge = {
      _pending: {}, _next: 0,
      postMessage(msg) { transport(String(JSON.stringify(msg))); },
      resolve(id, result) { var p = this._pending[id]; if (p) { p.resolve(result); delete this._pending[id]; } },
      reject(id, err) { var p = this._pending[id]; if (p) { p.reject(new Error(err)); delete this._pending[id]; } },
      call(name, args) { return new Promise((resolve, reject) => {
        var id = ++this._next;
        this._pending[id] = {resolve: resolve, reject: reject};
        this.postMessage({id: id, name: name, args: args});
      }); }
    };
  }
  setup();
  var t = setInterval(function() { setup(); if (window.__wvInstalled) clearInterval(t); }, 50);
})();`
}

// stubJS adds a window.<name> callable for each bound function. Evaluated after
// each navigation (when the bound set may have changed).
func stubJS(names []string) string {
	s := ""
	for _, name := range names {
		lit, _ := json.Marshal(name)
		s += "window[" + string(lit) + "] = function() { return window.webviewBridge.call(" + string(lit) + ", Array.prototype.slice.call(arguments)); };"
	}
	return s
}

// injectBridge (re)installs per-name stubs for the functions bound so far.
func (c *Chrome) injectBridge() {
	if c.BoundFuncs == nil {
		return
	}
	if s := stubJS(c.BoundFuncs()); s != "" {
		c.Eval(s)
	}
}
