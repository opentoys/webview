//go:build darwin

package darwin

// NSWindow and WKWebView construction.

import (
	"errors"
	"strconv"

	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/debuglog"
)

func (p *Platform) setupDataStore() objc.ID {
	if p.Incognito {
		return objc.ID(wkDataStoreClass).Send(nonPersistentDataStoreSel)
	}
	return objc.ID(wkDataStoreClass).Send(defaultDataStoreSel)
}

// beginOffscreenActivity prevents App Nap from throttling the process while a
// caller intentionally renders content without a visible window. It is ended
// when Run returns.
func (p *Platform) beginOffscreenActivity() {
	if !p.Offscreen || p.offscreenActivity != 0 {
		return
	}
	processInfo := objc.ID(objc.GetClass("NSProcessInfo")).Send(objc.RegisterName("processInfo"))
	if processInfo == 0 {
		return
	}
	// NSActivityUserInitiatedAllowingIdleSystemSleep: resist App Nap without
	// preventing the user's Mac from sleeping.
	const nsActivityUserInitiatedAllowingIdleSystemSleep = 0x00FFFFFF
	p.offscreenActivity = processInfo.Send(
		objc.RegisterName("beginActivityWithOptions:reason:"),
		nsActivityUserInitiatedAllowingIdleSystemSleep,
		nsString("webview offscreen rendering"),
	)
}

func (p *Platform) endOffscreenActivity() {
	if p.offscreenActivity == 0 {
		return
	}
	objc.ID(objc.GetClass("NSProcessInfo")).Send(
		objc.RegisterName("processInfo"),
	).Send(objc.RegisterName("endActivity:"), p.offscreenActivity)
	p.offscreenActivity = 0
}

// setup creates the NSWindow and WKWebView, then shows them. Runs on the host
// thread (called via mainThread from Run).
func (p *Platform) setup() error {
	p.beginOffscreenActivity()
	// Route script messages of the (process-global) handler class to this
	// platform. Called on the host thread; the didReceiveMessage handler
	// closure reads activePlatform on the same thread, so no lock is needed.
	activePlatform = p

	// Delegate: one per Platform, kept alive as a field.
	delegate := objc.ID(windowDelegateClass).Send(allocSel)
	delegate = delegate.Send(initSel)
	p.delegate = delegate

	w := objc.ID(nsWindowClass).Send(allocSel)
	if w == 0 {
		return errNoWindow
	}
	styleMask := styleTitled | styleClosable | styleResizable
	w = w.Send(initWithContentRectSel,
		rect(0, 0, 800, 600), styleMask, backingBuffered, false)
	p.mu.Lock()
	p.window = w
	p.mu.Unlock()
	w.Send(setDelegateSel, delegate)

	// Apply a size configured before Run while the window is still hidden.
	p.mu.Lock()
	width, height := p.pendingW, p.pendingH
	hint, hasSize := p.pendingSizeHint, p.hasPendingSize
	p.hasPendingSize = false
	p.mu.Unlock()
	if hasSize {
		applySizeOnHost(w, width, height, hint)
	}

	// UCC receives script messages. addScriptMessageHandler:name: does not retain
	// the handler, so keep both the UCC and handler alive on the Platform.
	ucc := objc.ID(wkUCCClass).Send(allocSel)
	if ucc == 0 {
		return errors.New("darwin: failed to alloc WKUserContentController")
	}
	ucc = ucc.Send(initSel)
	p.ucc = ucc
	handler := objc.ID(messageHandlerClass).Send(allocSel)
	handler = handler.Send(initSel)
	scriptHandler = handler
	ucc.Send(addScriptMessageHandlerSel, handler, nsString("webviewBridge"))

	// Inject any user scripts accumulated before Run().
	p.rebuildScriptsLocked()
	// WKURLSchemeTask drops POST bodies for some Blob/File fetches. Install a
	// document-start shim for registered schemes before any page code runs.
	if len(p.schemeHandlers) > 0 {
		addWKUserScript(ucc, fetchShimScript(p.schemeHandlers))
	}
	// Re-inject bootstrap with current bound func names (BootstrapFuncs may
	// have changed via Bind before Run).
	if p.BoundFuncs != nil {
		p.injectBootstrapLocked()
	}

	config := objc.ID(wkWebViewConfigClass).Send(allocSel)
	config = config.Send(initSel)
	config.Send(setUserContentControllerSel, ucc)
	// Website data store: incognito (non-persistent), custom dir, or default.
	config.Send(setWebsiteDataStoreSel, p.setupDataStore())
	// Register custom URL scheme handlers on the configuration.
	// Must happen before WKWebView is created. WebKit treats a scheme
	// registered through WKURLSchemeHandler as a secure context, so pages
	// loaded from it report window.isSecureContext == true. Do not spoof
	// that read-only browser value in injected JavaScript: doing so would not
	// grant the secure-context-only web APIs.
	for scheme, handler := range p.schemeHandlers {
		schemeHandlerInstances[scheme] = handler
		inst := objc.ID(schemeHandlerClass).Send(allocSel)
		inst = inst.Send(initSel)
		config.Send(setURLSchemeHandlerForURLSchemeSel, inst, nsString(scheme))
	}

	// Enable WebKit Inspector (right-click → Inspect Element) when Debug is set.
	if p.Debug {
		prefs := config.Send(preferencesSel)
		yesNum := objc.ID(objc.GetClass("NSNumber")).Send(numberWithBoolSel, true)
		// Private preference that exposes the "Inspect Element" context menu item.
		prefs.Send(setValueForKeySel, yesNum, nsString("developerExtrasEnabled"))
	}
	wv := objc.ID(wkWebViewClass).Send(allocSel)
	if wv == 0 {
		return errNoWebView
	}
	wv = wv.Send(initWithFrameSel, rect(0, 0, 800, 600), config)
	// macOS 13.3+ disables inspection by default; explicitly enable it.
	// https://webkit.org/blog/13936/enabling-the-inspection-of-web-content-in-apps/
	if p.Debug {
		wv.Send(setInspectableSel, true)
	}
	p.mu.Lock()
	p.webview = wv
	p.mu.Unlock()
	p.Logger.Log(BackendName, "ready", nil)

	// WKUIDelegate handles JS alert/confirm/prompt.
	uiDelegate := objc.ID(uiDelegateClass).Send(allocSel)
	uiDelegate = uiDelegate.Send(initSel)
	p.uiDelegate = uiDelegate
	wv.Send(setUIDelegateSel, uiDelegate)

	// WKNavigationDelegate intercepts downloads; WKDownloadDelegate handles
	// per-download save panel + completion.
	dlDelegate := objc.ID(downloadDelegateClass).Send(allocSel)
	dlDelegate = dlDelegate.Send(initSel)
	p.downloadDelegate = dlDelegate
	wv.Send(setNavigationDelegateSel, dlDelegate)

	// Wrap the webview in a container NSView so the Web Inspector pane can be
	// added as a sibling (matches reference cocoa_webkit.hh set_up_widget).
	widget := objc.ID(nsViewClass).Send(allocSel)
	if widget == 0 {
		return errors.New("darwin: failed to alloc NSView")
	}
	widget = widget.Send(initWithFrameOnlySel, rect(0, 0, 800, 600))
	widget.Send(setAutoresizesSubviewsSel, true)
	wv.Send(setAutoresizingMaskSel, webviewAutoresizingMask)
	widget.Send(addSubviewSel, wv)

	w.Send(setContentViewSel, widget)
	w.Send(centerSel)
	w.Send(makeKeyAndOrderFrontSel, 0)
	// Make webview first responder so it receives keyboard/mouse events.
	w.Send(objc.RegisterName("makeFirstResponder:"), wv)
	// Re-activate app to ensure window gets focus on modern macOS.
	objc.ID(nsAppClass).Send(sharedApplicationSel).Send(activateIgnoringOtherAppsSel, true)
	// Apply the menu bar (SetMenus before Run() stores it in pendingMenus;
	// buildPlatform wires DefaultMenus(w) through that same path so all
	// platforms share one entry point).
	p.applyMenus(p.pendingMenus)

	// Apply a title set before Run().
	p.mu.Lock()
	title := p.pendingTitle
	p.pendingTitle = ""
	p.mu.Unlock()
	if title != "" {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, title)
		w.Send(setTitleSel, str)
	}

	// Apply HTML set before Run() (the webview now exists).
	p.mu.Lock()
	html := p.pendingHTML
	p.pendingHTML = ""
	p.mu.Unlock()
	if html != "" {
		html = prependBootstrap(html)
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, html)
		wv.Send(loadHTMLStringSel, str, objc.ID(0))
		p.Logger.Log(BackendName, "load_html", map[string]string{"bytes": strconv.Itoa(len(html)), "phase": "started"})
	}

	// Apply a URL set before Run() (the webview now exists). Tighter priority
	// than pending HTML: a pending HTML page wins, then pending URL (and any
	// WKUserScript still fires for the empty document loadRequest starts with).
	p.mu.Lock()
	url := p.pendingURL
	p.pendingURL = ""
	p.mu.Unlock()
	if url != "" {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, url)
		nsURL := objc.ID(nsURLClass).Send(URLWithStringSel, str)
		req := objc.ID(nsURLRequestClass).Send(requestWithURLSel, nsURL)
		wv.Send(loadRequestSel, req)
		p.Logger.Log(BackendName, "navigate", map[string]string{"url": debuglog.URL(url), "phase": "started"})
	}
	return nil
}
