//go:build linux

package linux

import "encoding/json"

// --- user scripts ----------------------------------------------------------

func (p *gtk) pushUserScript(src string) {
	p.mu.Lock()
	p.userScriptSrcs = append(p.userScriptSrcs, src)
	ready := p.manager != 0
	p.mu.Unlock()
	if ready {
		p.MainThread(p.rebuildScripts)
	}
}

func (p *gtk) rebuildScripts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.manager == 0 {
		return
	}
	webkitUserContentManagerRemoveAllScripts(p.manager)
	addUserScript(p.manager, bootstrapJS(nil))
	for _, src := range p.userScriptSrcs {
		addUserScript(p.manager, src)
	}
	// Add bind script for currently bound functions.
	if p.BoundFuncs != nil {
		names := p.BoundFuncs()
		if len(names) > 0 {
			addUserScript(p.manager, bindScript(names))
		}
	}
}

func addUserScript(manager uintptr, src string) {
	script := webkitUserScriptNew(src, injectTopFrame, injectAtDocumentStart, 0, 0)
	webkitUserContentManagerAddScript(manager, script)
	webkitUserScriptUnref(script)
}

// bindScript returns JS that creates window.<name> stubs for each bound func.
func bindScript(names []string) string {
	s := ""
	for _, name := range names {
		lit := marshalJSON(name)
		s += "window[" + lit + "] = function() { return window.webviewBridge.call(" + lit + ", Array.prototype.slice.call(arguments)); };"
	}
	return s
}

// --- message routing -------------------------------------------------------

func (p *gtk) onMessage(body string) {
	if p.MessageFunc != nil {
		p.MessageFunc(body)
	}
}

func pgtk4MessageValue(p *gtk, arg uintptr) string {
	cs := jscValueToString(arg)
	s := cstr(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

func pgtk4RegisterScriptHandler(p *gtk, manager uintptr, name string) {
	webkitRegisterHandler3(manager, name, 0)
}

// --- helpers ---------------------------------------------------------------

func marshalJSON(msg string) string {
	data, _ := json.Marshal(msg)
	return string(data)
}
