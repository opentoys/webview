package darwin

import (
	"errors"
	"net/url"
	"runtime"
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

	config := objc.ID(objc.GetClass("WKWebViewConfiguration")).Send(objc.RegisterName("alloc"))
	config = config.Send(objc.RegisterName("init"))
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

func (p *Platform) SetHTML(html string) error {
	return p.Navigate("data:text/html;charset=utf-8," + url.PathEscape(html))
}

// evalJS runs JS without a completion handler (fire-and-forget).
func (p *Platform) evalJS(js string) {
	mainThread(func() {
		p.mu.Lock()
		wv := p.webview
		p.mu.Unlock()
		if wv == 0 {
			return
		}
		str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), js)
		wv.Send(objc.RegisterName("evaluateJavaScript:completionHandler:"), str, objc.ID(0))
	})
}

func (p *Platform) Eval(js string) error {
	p.evalJS(js)
	return nil
}

func (p *Platform) Dialog(kind DialogKind, message, defaultInput string) (string, bool) {
	return defaultInput, false
}
