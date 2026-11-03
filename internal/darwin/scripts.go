//go:build darwin

package darwin

// HTML loading, JavaScript evaluation, and user-script injection.

import (
	"errors"
	"strconv"

	"github.com/ebitengine/purego/objc"
)

func (p *Platform) SetHTML(html string) error {
	p.mu.Lock()
	wv := p.webview
	if wv == 0 {
		// No webview yet (called before Run): remember the HTML and load it
		// once setup() creates the webview.
		p.pendingHTML = html
		p.mu.Unlock()
		p.Logger.Log(BackendName, "load_html", map[string]string{"bytes": strconv.Itoa(len(html)), "phase": "queued"})
		return nil
	}
	p.mu.Unlock()
	html = p.prependBootstrap(html)
	// Use loadHTMLString:baseURL: to avoid data: URL encoding issues.
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, html)
		wv.Send(loadHTMLStringSel, str, objc.ID(0))
	})
	p.Logger.Log(BackendName, "load_html", map[string]string{"bytes": strconv.Itoa(len(html)), "phase": "started"})
	return nil
}

// evalJS runs JS without a completion handler (fire-and-forget), blocking on
// the host thread.
func (p *Platform) evalJS(js string) {
	mainThread(func() { p.evalOnHost(js) })
}

// evalOnHost runs JS on the host thread; must be called from the host thread.
func (p *Platform) evalOnHost(js string) {
	p.mu.Lock()
	wv := p.webview
	p.mu.Unlock()
	if wv == 0 {
		return
	}
	str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, js)
	wv.Send(evaluateJSSel, str, objc.ID(0))
}

// EvalHost queues js to run on the host thread without blocking. Safe from any
// goroutine including host-thread callbacks (e.g. MessageFunc). Uses a
// non-blocking channel send + performSelectorOnMainThread:NO: if called from
// the host thread, the ObjC selector fires after the current callback returns;
// if from another thread, it fires on the next run loop iteration.
func (p *Platform) EvalHost(js string) { go mainThread(func() { p.evalOnHost(js) }) }

func (p *Platform) Eval(js string) error {
	p.mu.Lock()
	ready := p.webview != 0 && !p.closed
	p.mu.Unlock()
	if !ready {
		return errors.New("webview: Eval requires a running window; use Init before Run")
	}
	p.evalJS(js)
	return nil
}

const wkInjectionTimeAtDocumentStart = 0

// Init registers JS to run at document start for every page load.
func (p *Platform) Init(js string) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("webview: Init after Close")
	}
	p.userScriptSrcs = append(p.userScriptSrcs, js)
	ready := p.ucc != 0
	p.mu.Unlock()
	if ready {
		mainThread(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if !p.closed {
				p.rebuildScriptsLocked()
			}
		})
	}
	return nil
}

// rebuildScriptsLocked re-injects all user scripts into the UCC.
// Caller must hold p.mu.
func (p *Platform) rebuildScriptsLocked() {
	p.ucc.Send(removeAllUserScriptsSel)
	if p.BoundFuncs != nil || p.BindingScript != nil {
		p.injectBootstrapLocked()
	}
	if len(p.schemeHandlers) > 0 {
		addWKUserScript(p.ucc, fetchShimScript(p.schemeHandlers))
	}
	for _, src := range p.userScriptSrcs {
		addWKUserScript(p.ucc, src)
	}
}

// addWKUserScript adds a WKUserScript to the given WKUserContentController.
func addWKUserScript(ucc objc.ID, src string) {
	s := objc.ID(wkUserScriptClass).Send(allocSel)
	s = s.Send(initWithSourceInjectionTimeForMainFrameOnlySel, nsString(src), wkInjectionTimeAtDocumentStart, true)
	ucc.Send(addUserScriptSel, s)
	s.Send(releaseSel)
}

// boundFuncNames returns the current bound function names from BoundFuncs.
func (p *Platform) boundFuncNames() []string {
	if p.BoundFuncs != nil {
		return p.BoundFuncs()
	}
	return nil
}
