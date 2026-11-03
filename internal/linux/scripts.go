//go:build linux

package linux

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
	addUserScript(p.manager, p.bootstrapScript())
	for _, src := range p.userScriptSrcs {
		addUserScript(p.manager, src)
	}
}

func addUserScript(manager uintptr, src string) {
	script := webkitUserScriptNew(src, injectTopFrame, injectAtDocumentStart, 0, 0)
	webkitUserContentManagerAddScript(manager, script)
	webkitUserScriptUnref(script)
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

func (p *gtk) RefreshBindings() error {
	p.mu.Lock()
	ready := p.manager != 0
	p.mu.Unlock()
	if !ready {
		return nil
	}
	p.MainThread(p.rebuildScripts)
	if p.BindingScript != nil {
		return p.Eval(p.BindingScript())
	}
	return nil
}

func (p *gtk) bootstrapScript() string {
	var names []string
	if p.BoundFuncs != nil {
		names = p.BoundFuncs()
	}
	src := bootstrapJS(names)
	if p.BindingScript != nil {
		src += p.BindingScript()
	}
	return src
}
