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

// scriptHandler keeps the active message handler instance alive. addScriptMessageHandler:
// does not retain its handler, so it must outlive the UCC or messages stop.
var scriptHandler objc.ID

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

	// windowShouldClose: returns whether the window should close when the user
	// clicks the close button. The window is the sender (one argument).
	windowShouldClose := func(id objc.ID, cmd objc.SEL, sender objc.ID) bool {
		return true
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
		body := objc.ID(message).Send(objc.RegisterName("body"))
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
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	if app == 0 {
		panic("darwin: no shared NSApplication")
	}
	app.Send(objc.RegisterName("setActivationPolicy:"), activationRegular)
	app.Send(objc.RegisterName("activateIgnoringOtherApps:"), true)
	close(hostReady)

	// nsdefaultMode is the non-blocking select on the loop's event poll.
	nsdefaultMode := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), "kCFRunLoopDefaultMode")
	// This manual loop never runs [NSApp run], so AppKit never drains its own
	// autorelease pool. Each iteration allocates autoreleased objects (the
	// poll's NSDate, any dequeued events), so drain a pool every iteration to
	// avoid unbounded memory growth.
	newSel := objc.RegisterName("new")
	drainSel := objc.RegisterName("drain")
	poolClass := objc.ID(objc.GetClass("NSAutoreleasePool"))
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
			until := objc.ID(objc.GetClass("NSDate")).Send(objc.RegisterName("dateWithTimeIntervalSinceNow:"), 0.05)
			event := app.Send(
				objc.RegisterName("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				eventMaskAny, until, nsdefaultMode, true,
			)
			if event != 0 {
				app.Send(objc.RegisterName("sendEvent:"), event)
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
	window   objc.ID
	webview  objc.ID
	delegate objc.ID
	// ucc keeps the WKUserContentController alive; the handler instance lives
	// in the package-level scriptHandler (registered class is process-global).
	ucc objc.ID

	// MessageFunc is called with the string body of each JS postMessage to
	// window.webkit.messageHandlers.webviewBridge.
	MessageFunc func(string)
	// BoundFuncs returns the JS-visible func names; the bootstrap script
	// defines window.<name> stubs from it at page start.
	BoundFuncs func() []string

	mu     sync.Mutex
	closed bool
	// pendingTitle is set by SetTitle before the window exists and applied in
	// setup(), so a title set before Run() is not lost.
	pendingTitle string
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
		mainThread(func() { window.Send(objc.RegisterName("orderOut:"), 0) })
	}
	return nil
}

// setup creates the NSWindow and WKWebView, then shows them. Runs on the host
// thread (called via mainThread from Run).
func (p *Platform) setup() error {
	// Route script messages of the (process-global) handler class to this
	// platform. Called on the host thread; the didReceiveMessage handler
	// closure reads activePlatform on the same thread, so no lock is needed.
	activePlatform = p

	// Delegate: one per Platform, kept alive as a field.
	delegate := objc.ID(windowDelegateClass).Send(objc.RegisterName("alloc"))
	delegate = delegate.Send(objc.RegisterName("init"))
	p.delegate = delegate

	w := objc.ID(objc.GetClass("NSWindow")).Send(objc.RegisterName("alloc"))
	if w == 0 {
		return errNoWindow
	}
	styleMask := styleTitled | styleClosable | styleResizable
	w = w.Send(objc.RegisterName("initWithContentRect:styleMask:backing:defer:"),
		rect(0, 0, 800, 600), styleMask, backingBuffered, false)
	p.mu.Lock()
	p.window = w
	p.mu.Unlock()
	w.Send(objc.RegisterName("setDelegate:"), delegate)

	// UCC receives script messages. addScriptMessageHandler:name: does not retain
	// the handler, so keep both the UCC and handler alive on the Platform.
	ucc := objc.ID(objc.GetClass("WKUserContentController")).Send(objc.RegisterName("alloc"))
	if ucc == 0 {
		return errors.New("darwin: failed to alloc WKUserContentController")
	}
	ucc = ucc.Send(objc.RegisterName("init"))
	p.ucc = ucc
	handler := objc.ID(messageHandlerClass).Send(objc.RegisterName("alloc"))
	handler = handler.Send(objc.RegisterName("init"))
	scriptHandler = handler
	ucc.Send(objc.RegisterName("addScriptMessageHandler:name:"), handler, nsString("webviewBridge"))

	config := objc.ID(objc.GetClass("WKWebViewConfiguration")).Send(objc.RegisterName("alloc"))
	config = config.Send(objc.RegisterName("init"))
	config.Send(objc.RegisterName("setUserContentController:"), ucc)
	wv := objc.ID(objc.GetClass("WKWebView")).Send(objc.RegisterName("alloc"))
	if wv == 0 {
		return errNoWebView
	}
	wv = wv.Send(objc.RegisterName("initWithFrame:configuration:"), rect(0, 0, 800, 600), config)
	p.mu.Lock()
	p.webview = wv
	p.mu.Unlock()

	w.Send(objc.RegisterName("setContentView:"), wv)
	w.Send(objc.RegisterName("center"))
	w.Send(objc.RegisterName("makeKeyAndOrderFront:"), 0)

	// Apply a title set before Run().
	p.mu.Lock()
	title := p.pendingTitle
	p.pendingTitle = ""
	p.mu.Unlock()
	if title != "" {
		str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), title)
		w.Send(objc.RegisterName("setTitle:"), str)
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
		str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), title)
		w.Send(objc.RegisterName("setTitle:"), str)
	})
	return nil
}

func (p *Platform) SetSize(width, height int, hint SizeHint) {
}

func (p *Platform) Navigate(url string) error {
	mainThread(func() {
		p.mu.Lock()
		wv := p.webview
		p.mu.Unlock()
		if wv == 0 {
			return
		}
		str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), url)
		nsURL := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("URLWithString:"), str)
		req := objc.ID(objc.GetClass("NSURLRequest")).Send(objc.RegisterName("requestWithURL:"), nsURL)
		wv.Send(objc.RegisterName("loadRequest:"), req)
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

func (p *Platform) SetHTML(html string) error {
	// Prepend the bridge bootstrap (webviewBridge + func stubs) as an inline
	// <script> so it is available before any user-supplied script runs.
	if p.BoundFuncs != nil {
		if js := bootstrapJS(p.BoundFuncs()); js != "" {
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
	}
	// Use loadHTMLString:baseURL: to avoid data: URL encoding issues.
	mainThread(func() {
		p.mu.Lock()
		wv := p.webview
		p.mu.Unlock()
		if wv == 0 {
			return
		}
		str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), html)
		wv.Send(objc.RegisterName("loadHTMLString:baseURL:"), str, objc.ID(0))
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
	str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), js)
	wv.Send(objc.RegisterName("evaluateJavaScript:completionHandler:"), str, objc.ID(0))
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

// Bind satisfies the Platform interface; the actual registry lives on W's
// bridge. Bound funcs reach JS against this platform only during bootstrap
// (Task 6), so there is nothing to store here yet.
func (p *Platform) Bind(name string, fn any) error {
	return nil
}
