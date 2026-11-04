package chrome

import (
	"encoding/json"

	"github.com/opentoys/webview/internal/bridgejs"
)

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
 if (window.webviewBridge && typeof window.webviewBridge === "object" && typeof window.webviewBridge.call === "function") return;
 function setup() {
  var transport = window.webviewBridge;
  if (typeof transport !== 'function') return;
  ` + bridgejs.Transport(`transport(String(JSON.stringify(msg)));`) + `
 }
 setup();
 if (!window.webviewBridge || typeof window.webviewBridge !== "object" || typeof window.webviewBridge.call !== "function") {
  var t = setInterval(function() {
   setup();
   if (window.webviewBridge && typeof window.webviewBridge === "object" && typeof window.webviewBridge.call === "function") {
    clearInterval(t);
   }
  }, 50);
 }
})();`
}

// injectBridge (re)installs per-name stubs for the functions bound so far.
func (c *Chrome) injectBridge() {
	c.Eval(c.bindingScript())
}

func (c *Chrome) bindingScript() string {
	if c.BindingScript != nil {
		return c.BindingScript()
	}
	return bridgejs.Proxies(nil)
}

// RefreshBindings replaces document-start scripts and updates the live page.
func (c *Chrome) RefreshBindings() error {
	c.Lock()
	ready := c.initScriptsReady
	c.Unlock()
	if !ready {
		return nil
	}
	if err := c.rebuildBindingScript(); err != nil {
		return err
	}
	return c.Eval(c.bindingScript())
}

// scriptMu serializes script replacement with Init and live binding changes.
func (c *Chrome) rebuildBindingScript() error {
	c.scriptMu.Lock()
	defer c.scriptMu.Unlock()
	c.Lock()
	scripts := append([]string(nil), c.pendingInitScripts...)
	c.Unlock()
	// Keep Init sources as separate scripts, preserving directive prologues
	// and independent execution if one source throws.
	for _, id := range c.documentScriptIDs {
		if _, err := c.send("Page.removeScriptToEvaluateOnNewDocument", h{"identifier": id}); err != nil {
			return err
		}
	}
	c.documentScriptIDs = nil
	sources := append([]string{bootstrapJS() + c.bindingScript()}, scripts...)
	for _, src := range sources {
		result, err := c.send("Page.addScriptToEvaluateOnNewDocument", h{"source": src})
		if err != nil {
			return err
		}
		var added struct {
			Identifier string `json:"identifier"`
		}
		if err := json.Unmarshal(result, &added); err != nil {
			return err
		}
		c.documentScriptIDs = append(c.documentScriptIDs, added.Identifier)
	}
	return nil
}
