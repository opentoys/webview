//go:build darwin

package darwin

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	errNoWindow  = errors.New("darwin: failed to alloc NSWindow")
	errNoWebView = errors.New("darwin: failed to alloc WKWebView")
)

type DialogKind int

const (
	DialogAlert DialogKind = iota
	DialogConfirm
	DialogPrompt
)

type SizeHint int

const (
	SizeNone SizeHint = iota
	SizeMin
	SizeMax
	SizeFixed
)

// NSApplicationActivationPolicyRegular = 0.
const activationRegular = 0

// NSWindow styleMask bits (NSWindowStyleMask).
const (
	styleTitled    = 1 << 0
	styleClosable  = 1 << 1
	styleResizable = 1 << 3
)

// NSBackingStoreBuffered = 2.
const backingBuffered = 2

// NSEventMaskAny = NSUIntegerMax.
const eventMaskAny = ^uint(0)

// windowDelegateClass is registered once at package init; re-registering the
// same name panics.
var windowDelegateClass objc.Class

// messageHandlerClass receives JS postMessage calls. Registered once at package
// init, like windowDelegateClass.
var messageHandlerClass objc.Class

// uiDelegateClass handles JS alert/confirm/prompt dialogs via WKUIDelegate.
// Registered once at package init; one instance per Platform.
var uiDelegateClass objc.Class

// scriptHandler keeps the active message handler instance alive. addScriptMessageHandler:
// does not retain its handler, so it must outlive the UCC or messages stop.
var scriptHandler objc.ID

// Cached ObjC classes (avoids repeated hash-table lookups in GetClass).
var (
	nsStringClass          objc.Class
	nsURLClass             objc.Class
	nsURLRequestClass      objc.Class
	nsWindowClass          objc.Class
	nsAppClass             objc.Class
	nsDateClass            objc.Class
	nsAutoreleasePoolClass objc.Class
	wkUCCClass             objc.Class
	wkWebViewConfigClass   objc.Class
	wkWebViewClass         objc.Class
	nsMenuClass            objc.Class
	nsMenuItemClass        objc.Class
	wkDataStoreClass       objc.Class
	wkDataStoreConfigClass objc.Class
)

// Cached ObjC selectors (avoids repeated hash-table lookups in RegisterName).
var (
	allocSel                        objc.SEL
	initSel                         objc.SEL
	newSel                          objc.SEL
	drainSel                        objc.SEL
	UTF8StringSel                   objc.SEL
	stringWithUTF8Sel               objc.SEL
	bodySel                         objc.SEL
	setTitleSel                     objc.SEL
	loadRequestSel                  objc.SEL
	URLWithStringSel                objc.SEL
	requestWithURLSel               objc.SEL
	evaluateJSSel                   objc.SEL
	loadHTMLStringSel               objc.SEL
	orderOutSel                     objc.SEL
	setDelegateSel                  objc.SEL
	initWithContentRectSel          objc.SEL
	setContentViewSel               objc.SEL
	centerSel                       objc.SEL
	makeKeyAndOrderFrontSel         objc.SEL
	addScriptMessageHandlerSel      objc.SEL
	setUserContentControllerSel     objc.SEL
	initWithFrameSel                objc.SEL
	setUIDelegateSel                objc.SEL
	sharedApplicationSel            objc.SEL
	setActivationPolicySel          objc.SEL
	activateIgnoringOtherAppsSel    objc.SEL
	dateWithTimeIntervalSinceNowSel objc.SEL
	nextEventMatchingMaskSel        objc.SEL
	sendEventSel                    objc.SEL
	windowWillCloseSel              objc.SEL
	initWithTitleSel                objc.SEL
	initWithTitleOnlySel            objc.SEL
	autoreleaseSel                  objc.SEL
	separatorItemSel                objc.SEL
	setSubmenuSel                   objc.SEL
	setMainMenuSel                  objc.SEL
	addItemSel                      objc.SEL
	setWebsiteDataStoreSel          objc.SEL
	nonPersistentDataStoreSel       objc.SEL
	setDataStoreDirectoryURLSel     objc.SEL
	initWithConfigSel               objc.SEL
	defaultDataStoreSel             objc.SEL
)

// activePlatform is the Platform whose webview is currently set up. Process-
// global because handler methods are registered per-class, not per-instance.
// Written once in setup() on the host thread; read from the host thread the
// same way, so no lock is needed.
var activePlatform *Platform

func init() {
	// AppKit and WebKit are not linked into a CGO_ENABLED=0 binary, so load them
	// explicitly before looking up their classes.
	for _, fw := range []string{
		"/System/Library/Frameworks/Cocoa.framework/Cocoa",
		"/System/Library/Frameworks/WebKit.framework/WebKit",
	} {
		if _, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
			panic(err)
		}
	}

	// Cache frequently used ObjC classes and selectors.
	nsStringClass = objc.GetClass("NSString")
	nsURLClass = objc.GetClass("NSURL")
	nsURLRequestClass = objc.GetClass("NSURLRequest")
	nsWindowClass = objc.GetClass("NSWindow")
	nsAppClass = objc.GetClass("NSApplication")
	nsDateClass = objc.GetClass("NSDate")
	nsAutoreleasePoolClass = objc.GetClass("NSAutoreleasePool")
	wkUCCClass = objc.GetClass("WKUserContentController")
	wkWebViewConfigClass = objc.GetClass("WKWebViewConfiguration")
	wkWebViewClass = objc.GetClass("WKWebView")
	nsMenuClass = objc.GetClass("NSMenu")
	nsMenuItemClass = objc.GetClass("NSMenuItem")
	wkDataStoreClass = objc.GetClass("WKWebsiteDataStore")
	wkDataStoreConfigClass = objc.GetClass("WKWebsiteDataStoreConfiguration")

	allocSel = objc.RegisterName("alloc")
	initSel = objc.RegisterName("init")
	newSel = objc.RegisterName("new")
	drainSel = objc.RegisterName("drain")
	UTF8StringSel = objc.RegisterName("UTF8String")
	stringWithUTF8Sel = objc.RegisterName("stringWithUTF8String:")
	bodySel = objc.RegisterName("body")
	setTitleSel = objc.RegisterName("setTitle:")
	loadRequestSel = objc.RegisterName("loadRequest:")
	URLWithStringSel = objc.RegisterName("URLWithString:")
	requestWithURLSel = objc.RegisterName("requestWithURL:")
	evaluateJSSel = objc.RegisterName("evaluateJavaScript:completionHandler:")
	loadHTMLStringSel = objc.RegisterName("loadHTMLString:baseURL:")
	orderOutSel = objc.RegisterName("orderOut:")
	setDelegateSel = objc.RegisterName("setDelegate:")
	initWithContentRectSel = objc.RegisterName("initWithContentRect:styleMask:backing:defer:")
	setContentViewSel = objc.RegisterName("setContentView:")
	centerSel = objc.RegisterName("center")
	makeKeyAndOrderFrontSel = objc.RegisterName("makeKeyAndOrderFront:")
	addScriptMessageHandlerSel = objc.RegisterName("addScriptMessageHandler:name:")
	setUserContentControllerSel = objc.RegisterName("setUserContentController:")
	initWithFrameSel = objc.RegisterName("initWithFrame:configuration:")
	setUIDelegateSel = objc.RegisterName("setUIDelegate:")
	sharedApplicationSel = objc.RegisterName("sharedApplication")
	setActivationPolicySel = objc.RegisterName("setActivationPolicy:")
	activateIgnoringOtherAppsSel = objc.RegisterName("activateIgnoringOtherApps:")
	dateWithTimeIntervalSinceNowSel = objc.RegisterName("dateWithTimeIntervalSinceNow:")
	nextEventMatchingMaskSel = objc.RegisterName("nextEventMatchingMask:untilDate:inMode:dequeue:")
	sendEventSel = objc.RegisterName("sendEvent:")
	windowWillCloseSel = objc.RegisterName("windowWillClose:")
	initWithTitleSel = objc.RegisterName("initWithTitle:action:keyEquivalent:")
	initWithTitleOnlySel = objc.RegisterName("initWithTitle:")
	autoreleaseSel = objc.RegisterName("autorelease")
	separatorItemSel = objc.RegisterName("separatorItem")
	addItemSel = objc.RegisterName("addItem:")
	setSubmenuSel = objc.RegisterName("setSubmenu:")
	setMainMenuSel = objc.RegisterName("setMainMenu:")
	setWebsiteDataStoreSel = objc.RegisterName("setWebsiteDataStore:")
	nonPersistentDataStoreSel = objc.RegisterName("nonPersistentDataStore")
	setDataStoreDirectoryURLSel = objc.RegisterName("setDataStoreDirectoryURL:")
	initWithConfigSel = objc.RegisterName("initWithConfiguration:")
	defaultDataStoreSel = objc.RegisterName("defaultDataStore")

	// windowShouldClose: returns whether the window should close when the user
	// clicks the close button. The window is the sender (one argument).
	windowShouldClose := func(id objc.ID, cmd objc.SEL, sender objc.ID) bool {
		return true
	}
	// windowWillClose: signals Close() semantics when the user closes the window
	// via the titlebar button. Runs on the host thread (delegate callbacks are
	// delivered there), so call signalExit, not Close() (which would deadlock on
	// mainThread).
	windowWillClose := func(id objc.ID, cmd objc.SEL, window objc.ID) {
		if p := activePlatform; p != nil {
			p.signalExit()
		}
	}
	// applicationShouldTerminateAfterLastWindowClosed: keeps the app alive after
	// the last window closes so Run() only returns via Close().
	terminateAfterLastWindowClosed := func(id objc.ID, cmd objc.SEL, app objc.ID) bool {
		return false
	}
	var err error
	windowDelegateClass, err = objc.RegisterClass(
		"GoWebviewWindowDelegate",
		objc.GetClass("NSObject"),
		nil,
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("windowShouldClose:"), Fn: windowShouldClose},
			{Cmd: objc.RegisterName("windowWillClose:"), Fn: windowWillClose},
			{Cmd: objc.RegisterName("applicationShouldTerminateAfterLastWindowClosed:"), Fn: terminateAfterLastWindowClosed},
		},
	)
	if err != nil {
		panic(err)
	}

	// userContentController:didReceiveScriptMessage: is called on the host thread
	// when JS runs postMessage on this class's registered handler. ObjC method
	// closures can't capture Go state, so route through the package-level
	// activePlatform set in setup().
	didReceiveMessage := func(id objc.ID, cmd objc.SEL, controller objc.ID, message objc.ID) {
		p := activePlatform
		if p == nil || p.MessageFunc == nil {
			return
		}
		body := objc.ID(message).Send(bodySel)
		text := goString(body)
		p.MessageFunc(text)
	}
	messageHandlerClass, err = objc.RegisterClass(
		"GoWebviewScriptHandler",
		objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKScriptMessageHandler")},
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("userContentController:didReceiveScriptMessage:"), Fn: didReceiveMessage},
		},
	)
	if err != nil {
		panic(err)
	}

	// WKUIDelegate: handles JS alert/confirm/prompt. Runs on the WebKit
	// delivery thread, which is the host thread (same assumption as
	// MessageFunc/didReceiveScriptMessage). activePlatform is written once
	// in setup() on the host thread and read here on the host thread.
	uiDelegateClass, err = objc.RegisterClass(
		"GoWebviewUIDelegate",
		objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKUIDelegate")},
		nil,
		[]objc.MethodDef{
			// Alert: completion block takes no args.
			{Cmd: objc.RegisterName("webView:runJavaScriptAlertPanelWithMessage:initiatedByFrame:completionHandler:"),
				Fn: func(id objc.ID, sel objc.SEL, webView objc.ID, msg objc.ID, frame objc.ID, completion objc.ID) {
					text := goString(msg)
					dp := activePlatform
					if dp != nil && dp.DialogFunc != nil {
						dp.DialogFunc(DialogAlert, text, "")
					}
					callBlock(completion)
				}},
			// Confirm: completion block takes BOOL → pass true for OK.
			{Cmd: objc.RegisterName("webView:runJavaScriptConfirmPanelWithMessage:initiatedByFrame:completionHandler:"),
				Fn: func(id objc.ID, sel objc.SEL, webView objc.ID, msg objc.ID, frame objc.ID, completion objc.ID) {
					text := goString(msg)
					ok := false
					dp := activePlatform
					if dp != nil && dp.DialogFunc != nil {
						_, ok = dp.DialogFunc(DialogConfirm, text, "")
					}
					// BOOL as int64: on arm64 BOOL = signed char; purego
					// marshals Go int64 → C signed char via encodeType.
					var confirm int64
					if ok {
						confirm = 1
					}
					callBlock(completion, confirm)
				}},
			// Prompt: completion block takes NSString (or nil for cancel).
			{Cmd: objc.RegisterName("webView:runJavaScriptPromptWithPrompt:defaultText:initiatedByFrame:completionHandler:"),
				Fn: func(id objc.ID, sel objc.SEL, webView objc.ID, prompt objc.ID, defaultText objc.ID, frame objc.ID, completion objc.ID) {
					text := goString(prompt)
					def := goString(defaultText)
					dp := activePlatform
					if dp != nil && dp.DialogFunc != nil {
						result, ok := dp.DialogFunc(DialogPrompt, text, def)
						if ok {
							callBlock(completion, nsString(result))
						} else {
							callBlock(completion, objc.ID(0))
						}
						return
					}
					// Default: return the default text.
					callBlock(completion, nsString(def))
				}},
		},
	)
	if err != nil {
		panic(err)
	}
}

// AppKit work is thread-affine: the NSApplication adopts whichever thread first
// creates it, and all windows/views must be created on that same thread. The Go
// runtime can migrate goroutines between OS threads, and `go test` runs tests
// on arbitrary goroutines, so all AppKit calls go through a single dedicated
// host thread running a manual event loop.
var (
	hostOnce  sync.Once
	hostReady chan struct{} // closed once the host loop is running
	// hostCmdPtr is set under hostOnce before the host loop starts, so
	// mainThread reads it after Run() has finished starting the host.
	hostCmdPtr atomic.Pointer[chan func()]
)

// startAppHost launches the single AppKit host thread if not already running,
// then blocks until it is ready.
func startAppHost() {
	hostOnce.Do(func() {
		hostReady = make(chan struct{})
		cmd := make(chan func(), 64)
		hostCmdPtr.Store(&cmd)
		go hostLoop()
	})
	<-hostReady
}

// hostCmd returns the command channel, or nil before the host has started.
func hostCmd() chan func() {
	if p := hostCmdPtr.Load(); p != nil {
		return *p
	}
	return nil
}

// hostLoop runs on one pinned OS thread for the life of the process: it owns
// the NSApplication and pumps its event loop, running queued commands in
// between. This replaces [NSApp run], which would need [NSApp terminate:] to
// exit — and terminate: exits the whole process, breaking multi-window use.
func hostLoop() {
	runtime.LockOSThread()
	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	if app == 0 {
		panic("darwin: no shared NSApplication")
	}
	app.Send(setActivationPolicySel, activationRegular)
	app.Send(activateIgnoringOtherAppsSel, true)
	close(hostReady)

	// nsdefaultMode is the non-blocking select on the loop's event poll.
	nsdefaultMode := objc.ID(nsStringClass).Send(stringWithUTF8Sel, "kCFRunLoopDefaultMode")
	// This manual loop never runs [NSApp run], so AppKit never drains its own
	// autorelease pool. Each iteration allocates autoreleased objects (the
	// poll's NSDate, any dequeued events), so drain a pool every iteration to
	// avoid unbounded memory growth.
	poolClass := objc.ID(nsAutoreleasePoolClass)
	pool := poolClass.Send(newSel)
	cmd := hostCmd()
	for {
		// Run queued Go commands before blocking on events.
		select {
		case fn := <-cmd:
			fn()
		default:
			// Poll with a short timeout so commands are served promptly without a
			// 100% CPU busy loop.
			until := objc.ID(nsDateClass).Send(dateWithTimeIntervalSinceNowSel, 0.05)
			event := app.Send(
				nextEventMatchingMaskSel,
				eventMaskAny, until, nsdefaultMode, true,
			)
			if event != 0 {
				app.Send(sendEventSel, event)
			}
		}
		// Drain the previous iteration's pool and start a fresh one.
		pool.Send(drainSel)
		pool = poolClass.Send(newSel)
	}
}

// mainThread runs fn on the AppKit host thread, blocking until it completes.
// Before the host has started (no window exists yet) it is a no-op, which makes
// SetTitle/SetHTML/Navigate/Eval safe to call before Run().
func mainThread(fn func()) {
	cmd := hostCmd()
	if cmd == nil {
		return
	}
	done := make(chan struct{})
	cmd <- func() {
		fn()
		close(done)
	}
	<-done
}

type Platform struct {
	window     objc.ID
	webview    objc.ID
	delegate   objc.ID
	uiDelegate objc.ID // WKUIDelegate instance; kept alive for the webview.
	// ucc keeps the WKUserContentController alive; the handler instance lives
	// in the package-level scriptHandler (registered class is process-global).
	ucc objc.ID

	// MessageFunc is called with the string body of each JS postMessage to
	// window.webkit.messageHandlers.webviewBridge.
	MessageFunc func(string)
	// BoundFuncs returns the JS-visible func names; the bootstrap script
	// defines window.<name> stubs from it at page start.
	BoundFuncs func() []string
	// DialogFunc is called by the WKUIDelegate for JS alert/confirm/prompt.
	// Called on the host thread (same thread as MessageFunc).
	DialogFunc func(kind DialogKind, message, defaultInput string) (string, bool)

	// Incognito makes the webview use a non-persistent (in-memory) website data
	// store: no cookies/cache/localStorage written to disk.
	Incognito bool
	// DataDir sets the persistent website data store directory (cookies, cache,
	// localStorage). Empty = WebKit default. Ignored when Incognito is set.
	DataDir string

	mu     sync.Mutex
	closed bool
	// pendingTitle is set by SetTitle before the window exists and applied in
	// setup(), so a title set before Run() is not lost.
	pendingTitle string
	// pendingHTML is set by SetHTML before the webview exists and loaded in
	// setup(), so HTML set before Run() is not silently dropped.
	pendingHTML string
	// pendingURL is set by Navigate before the webview exists and loaded in
	// setup(), so a navigation set before Run() is not silently dropped.
	pendingURL string
	// runDone is closed by Close() to signal Run() to return.
	runDone chan struct{}
}

func New() *Platform {
	return &Platform{runDone: make(chan struct{})}
}

func (p *Platform) Run() error {
	startAppHost()
	var setupErr error
	mainThread(func() { setupErr = p.setup() })
	if setupErr != nil {
		return setupErr
	}
	<-p.runDone
	return nil
}

func (p *Platform) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.runDone)
	window := p.window
	p.mu.Unlock()

	// Hide the window so a closed platform doesn't linger on screen while the
	// next window in the same process is running.
	if window != 0 {
		mainThread(func() { window.Send(orderOutSel, 0) })
	}
	return nil
}

// signalExit makes Run() return without closing the window. Callable from the
// host thread (windowWillClose:) or any other thread (Close()). Uses a non-
// blocking channel send so it is safe on the host thread where Close()'s
// mainThread orderOut would deadlock.
func (p *Platform) signalExit() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	select {
	case p.runDone <- struct{}{}:
	default:
	}
}

// setupMainMenu installs a minimal main menu bar with an App menu and an Edit
// menu. Without a nib, a bare AppKit app has no menu, so Cmd-C/Cmd-V key
// equivalents (routed to the first responder via the main menu's Edit items)
// silently do nothing. Follows the reference webview_go implementation
// (webview.h: 2317-2348): each Edit item carries a real key equivalent
// ("x"/"c"/"v"/"a") so the responder chain resolves cut:/copy:/paste:/selectAll:.
// Runs on the host thread from setup().
func setupMainMenu() {
	menu := objc.ID(nsMenuClass).Send(allocSel)
	menu = menu.Send(initSel)

	// Application menu.
	appItem := objc.ID(nsMenuItemClass).Send(allocSel)
	appItem = appItem.Send(initWithTitleSel, nsString(""), 0, nsString(""))
	appMenu := objc.ID(nsMenuClass).Send(allocSel)
	appMenu = appMenu.Send(initWithTitleOnlySel, nsString(""))
	appMenu.Send(autoreleaseSel)
	appItem.Send(setSubmenuSel, appMenu)
	menu.Send(addItemSel, appItem)

	// Edit menu: Cut/Copy/Paste/Select All with Cmd shortcuts.
	editItem := objc.ID(nsMenuItemClass).Send(allocSel)
	editItem = editItem.Send(initWithTitleSel, nsString("Edit"), 0, nsString(""))
	editMenu := objc.ID(nsMenuClass).Send(allocSel)
	editMenu = editMenu.Send(initWithTitleOnlySel, nsString("Edit"))
	editMenu.Send(autoreleaseSel)
	editItem.Send(setSubmenuSel, editMenu)
	menu.Send(addItemSel, editItem)

	for _, e := range []struct{ title, action, key string }{
		{"Cut", "cut:", "x"},
		{"Copy", "copy:", "c"},
		{"Paste", "paste:", "v"},
	} {
		item := objc.ID(nsMenuItemClass).Send(allocSel)
		item = item.Send(initWithTitleSel, nsString(e.title), objc.RegisterName(e.action), nsString(e.key))
		editMenu.Send(addItemSel, item)
	}
	sep := objc.ID(nsMenuItemClass).Send(separatorItemSel)
	editMenu.Send(addItemSel, sep)
	selectAll := objc.ID(nsMenuItemClass).Send(allocSel)
	selectAll = selectAll.Send(initWithTitleSel, nsString("Select All"), objc.RegisterName("selectAll:"), nsString("a"))
	editMenu.Send(addItemSel, selectAll)

	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	app.Send(setMainMenuSel, menu)
}

// setupDataStore returns the WKWebsiteDataStore for the platform: a non-
// persistent (in-memory, incognito) store when Incognito is set, else the
// default persistent store or a persistent store rooted at DataDir. Runs on
// the host thread from setup().
func (p *Platform) setupDataStore() objc.ID {
	if p.Incognito {
		return objc.ID(wkDataStoreClass).Send(nonPersistentDataStoreSel)
	}
	if p.DataDir != "" {
		// Custom persistent store directory. WKWebsiteDataStore has no public
		// initializer (init/new are NS_UNAVAILABLE), so a directory-based store
		// requires the private "_initWithConfiguration:". Guard with
		// respondsToSelector: and fall back to the default store if absent.
		conf := objc.ID(wkDataStoreConfigClass).Send(allocSel)
		conf = conf.Send(initSel)
		dir := nsString(p.DataDir)
		nsURL := objc.ID(nsURLClass).Send(URLWithStringSel, dir)
		conf.Send(setDataStoreDirectoryURLSel, nsURL)
		const privInit = "_initWithConfiguration:"
		if objc.ID(wkDataStoreClass).Send(objc.RegisterName("respondsToSelector:"), objc.RegisterName(privInit)) != 0 {
			return objc.ID(wkDataStoreClass).Send(objc.RegisterName(privInit), conf)
		}
		// Private API unavailable on this OS: fall back to the default store.
		return objc.ID(wkDataStoreClass).Send(defaultDataStoreSel)
	}
	return objc.ID(wkDataStoreClass).Send(defaultDataStoreSel)
}

// setup creates the NSWindow and WKWebView, then shows them. Runs on the host
// thread (called via mainThread from Run).
func (p *Platform) setup() error {
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

	config := objc.ID(wkWebViewConfigClass).Send(allocSel)
	config = config.Send(initSel)
	config.Send(setUserContentControllerSel, ucc)
	// Website data store: incognito (non-persistent), custom dir, or default.
	config.Send(setWebsiteDataStoreSel, p.setupDataStore())
	wv := objc.ID(wkWebViewClass).Send(allocSel)
	if wv == 0 {
		return errNoWebView
	}
	wv = wv.Send(initWithFrameSel, rect(0, 0, 800, 600), config)
	p.mu.Lock()
	p.webview = wv
	p.mu.Unlock()

	// WKUIDelegate handles JS alert/confirm/prompt.
	uiDelegate := objc.ID(uiDelegateClass).Send(allocSel)
	uiDelegate = uiDelegate.Send(initSel)
	p.uiDelegate = uiDelegate
	wv.Send(setUIDelegateSel, uiDelegate)

	w.Send(setContentViewSel, wv)
	w.Send(centerSel)
	w.Send(makeKeyAndOrderFrontSel, 0)
	// Cmd-C/Cmd-V need an Edit menu (key equivalents route via the main menu).
	// Bare AppKit apps without a nib have no menu, so install one once.
	setupMainMenu()

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
	}
	return nil
}

func (p *Platform) SetTitle(title string) error {
	p.mu.Lock()
	if p.window == 0 {
		// No window yet (called before Run): remember the title and apply it
		// once setup() creates the window.
		p.pendingTitle = title
		p.mu.Unlock()
		return nil
	}
	w := p.window
	p.mu.Unlock()
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, title)
		w.Send(setTitleSel, str)
	})
	return nil
}

func (p *Platform) SetSize(width, height int, hint SizeHint) {
}

func (p *Platform) Navigate(url string) error {
	p.mu.Lock()
	wv := p.webview
	if wv == 0 {
		// No webview yet (called before Run): remember the URL and load it
		// once setup() creates the webview.
		p.pendingURL = url
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, url)
		nsURL := objc.ID(nsURLClass).Send(URLWithStringSel, str)
		req := objc.ID(nsURLRequestClass).Send(requestWithURLSel, nsURL)
		wv.Send(loadRequestSel, req)
	})
	return nil
}

// indexHead returns a byte index in html suitable for inserting a <script> tag
// so that it runs before any user-supplied script. Looks for <head>, </head>,
// <body>, or the first <script>, returning the index AFTER that tag or -1.
func indexHead(html string) int {
	lower := strings.ToLower(html)
	for _, tag := range []string{"<head>", "<head ", "<head\t", "<head\n"} {
		if i := strings.Index(lower, tag); i >= 0 {
			return i + len(tag)
		}
	}
	if i := strings.Index(lower, "</head>"); i >= 0 {
		return i
	}
	if i := strings.Index(lower, "<body"); i >= 0 {
		return i
	}
	if i := strings.Index(lower, "<script"); i >= 0 {
		return i
	}
	return -1
}

// boundFuncNames returns the JS-visible func names from the active platform's
// BoundFuncs. Used by prependBootstrap; activePlatform is set in setup() before
// any SetHTML path runs, so it is always current.
func boundFuncNames() []string {
	if p := activePlatform; p != nil && p.BoundFuncs != nil {
		return p.BoundFuncs()
	}
	return nil
}

// prependBootstrap inserts the bridge bootstrap (webviewBridge + func stubs) as
// an inline <script> so it is available before any user-supplied script runs.
func prependBootstrap(html string) string {
	if js := bootstrapJS(boundFuncNames()); js != "" {
		// HTML parsing closes <script> on </script>, </script>, or </SCRIPT>.
		// Escape </ sequences so the script body is safe inside the tag.
		js = strings.ReplaceAll(js, "</", `<\/`)
		tag := "<script>" + js + "</script>"
		if i := indexHead(html); i >= 0 {
			html = html[:i] + tag + html[i:]
		} else {
			html = tag + html
		}
	}
	return html
}

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

// EvalHost queues js to run on the host thread without blocking, so it is safe
// from any thread including a MessageFunc callback on the host thread (where
// mainThread would deadlock).
func (p *Platform) EvalHost(js string) {
	cmd := hostCmd()
	if cmd == nil {
		return
	}
	cmd <- func() { p.evalOnHost(js) }
}

func (p *Platform) Eval(js string) error {
	p.evalJS(js)
	return nil
}

func (p *Platform) Dialog(kind DialogKind, message, defaultInput string) (string, bool) {
	return defaultInput, false
}
