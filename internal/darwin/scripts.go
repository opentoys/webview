//go:build darwin

package darwin

// HTML loading, JavaScript evaluation, and user-script injection.

import "github.com/ebitengine/purego/objc"

func (p *Platform) SetHTML(html string) error {
	p.mu.Lock()
	wv := p.webview
	if wv == 0 {
		// No webview yet (called before Run): remember the HTML and load it
		// once setup() creates the webview.
		p.pendingHTML = html
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	html = prependBootstrap(html)
	// Use loadHTMLString:baseURL: to avoid data: URL encoding issues.
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, html)
		wv.Send(loadHTMLStringSel, str, objc.ID(0))
	})
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
func (p *Platform) EvalHost(js string) {
	select {
	case <-hostReady:
	default:
		return
	}
	select {
	case commandChan <- func() { p.evalOnHost(js) }:
		// Command queued; trigger the ObjC selector to read and execute it.
		objc.ID(commandHandlerClass).Send(allocSel).Send(
			performSelectorOnMainThreadWithObjectWaitUntilDoneSel,
			objc.RegisterName("runCommand:"),
			0, false)
	default:
		// Channel full (unlikely) — run directly. Safe when called from the
		// host thread (MessageFunc callback). On other threads this is a race,
		// but channel-full implies 64+ pending evals, which is a bug anyway.
		p.evalOnHost(js)
	}
}

func (p *Platform) Eval(js string) error {
	p.evalJS(js)
	return nil
}

const wkInjectionTimeAtDocumentStart = 0

// Init registers JS to run at document start for every page load.
func (p *Platform) Init(js string) error {
	p.mu.Lock()
	p.userScriptSrcs = append(p.userScriptSrcs, js)
	if p.ucc != 0 {
		p.rebuildScriptsLocked()
	}
	p.mu.Unlock()
	return nil
}

// rebuildScriptsLocked re-injects all user scripts into the UCC.
// Caller must hold p.mu.
func (p *Platform) rebuildScriptsLocked() {
	p.ucc.Send(removeAllUserScriptsSel)
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
