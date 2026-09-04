//go:build darwin

package darwin

// Platform lifecycle and AppKit host-thread dispatch.

import (
	"runtime"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/debuglog"
)

var (
	hostOnce  sync.Once
	hostReady chan struct{} // closed once the host loop is running
)

// startAppHost launches the single AppKit host thread if not already running,
// then blocks until it is ready.
func startAppHost() {
	hostOnce.Do(func() {
		hostReady = make(chan struct{})
		go hostLoop()
	})
	<-hostReady
}

// hostLoop runs on one pinned OS thread for the life of the process: it owns
// the NSApplication and pumps its event loop via [NSApp run], which processes
// all events, timers, and the main dispatch queue. Cross-thread commands arrive
// via performSelectorOnMainThread: (which [NSApp run] dispatches from the run
// loop) and are read from the commandChan.
func hostLoop() {
	runtime.LockOSThread()
	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	if app == 0 {
		panic("darwin: no shared NSApplication")
	}
	app.Send(setActivationPolicySel, activationRegular)
	app.Send(activateIgnoringOtherAppsSel, true)
	// Complete app launch (menu/activation setup) as a nib-less program must.
	app.Send(finishLaunchingSel)
	close(hostReady)
	// Pump the run loop. [NSApp run] processes all events and dispatches
	// performSelectorOnMainThread: calls (which read from commandChan and
	// execute the queued Go functions).
	app.Send(objc.RegisterName("run"))
}

// mainThread runs fn on the AppKit host thread, blocking until it completes.
// Before the host has started (no window exists yet) it is a no-op, which makes
// SetTitle/SetHTML/Navigate/Eval safe to call before Run().
func mainThread(fn func()) {
	select {
	case <-hostReady:
	default:
		return
	}
	commandChan <- fn
	objc.ID(commandHandlerClass).Send(allocSel).Send(
		performSelectorOnMainThreadWithObjectWaitUntilDoneSel,
		objc.RegisterName("runCommand:"),
		0, true)
}

type Platform struct {
	window           objc.ID
	webview          objc.ID
	delegate         objc.ID
	uiDelegate       objc.ID // WKUIDelegate instance; kept alive for the webview.
	downloadDelegate objc.ID // WKNavigationDelegate+WKDownloadDelegate instance.
	// ucc keeps the WKUserContentController alive; the handler instance lives
	// in the package-level scriptHandler (registered class is process-global).
	ucc objc.ID

	// MessageFunc is called with the string body of each JS postMessage to
	// window.webkit.messageHandlers.webviewBridge.
	MessageFunc func(string)
	// BoundFuncs returns the JS-visible func names; the bootstrap script
	// defines window.<name> stubs from it at page start.
	BoundFuncs func() []string
	// OpenPanelFunc overrides the native NSOpenPanel sheet for <input type=file>.
	// When set, WebKit does not show the default panel; the app must call
	// callback with the absolute paths the user chose, or (nil,false) to
	// cancel. callback is async and safe from any goroutine.
	OpenPanelFunc func(params OpenPanelParams, callback func(paths []string, ok bool))
	// DownloadFunc overrides the native NSSavePanel for file downloads.
	// When set, the app must call callback with the absolute save path,
	// or "" to cancel. callback is async and safe from any goroutine.
	DownloadFunc func(suggestedFilename string, callback func(savePath string))

	// Debug enables WebKit Inspector (right-click → Inspect Element) on macOS
	// and dev tools on Windows. Set via Options.Debug.
	Debug  bool
	Logger *debuglog.Logger
	// Incognito makes the webview use a non-persistent (in-memory) website data
	// store: no cookies/cache/localStorage written to disk.
	Incognito bool
	// DataDir sets the persistent website data store directory (cookies, cache,
	// localStorage). Empty = WebKit default. Ignored when Incognito is set.
	DataDir string
	// Offscreen keeps the process out of App Nap while the window is minimized
	// or occluded, allowing WebKit rendering and timers to continue.
	Offscreen         bool
	offscreenActivity objc.ID

	mu     sync.Mutex
	closed bool
	// pendingTitle is set by SetTitle before the window exists and applied in
	// setup(), so a title set before Run() is not lost.
	pendingTitle string
	// Pending size state is applied before setup() shows the window.
	pendingW        int
	pendingH        int
	pendingSizeHint SizeHint
	hasPendingSize  bool
	// pendingHTML is set by SetHTML before the webview exists and loaded in
	// setup(), so HTML set before Run() is not silently dropped.
	pendingHTML string
	// pendingURL is set by Navigate before the webview exists and loaded in
	// setup(), so a navigation set before Run() is not silently dropped.
	pendingURL string
	// runDone is closed by Close() to signal Run() to return.
	runDone chan struct{}
	// schemeHandlers stores registered resource handlers, keyed by scheme
	// name (without "://"). Populated before Run() via InterceptResource,
	// wired to WKWebViewConfiguration in setup().
	schemeHandlers map[string]ResourceHandler
	// userScriptSrcs accumulates JS sources added via Init(). They are
	// injected into WKUserContentController so they run at document start
	// for every page load.
	userScriptSrcs []string

	// pendingMenus stores menus set via SetMenus before Run().
	pendingMenus []Menu
}

func New() (*Platform, error) {
	probe()
	return &Platform{
		runDone:        make(chan struct{}),
		schemeHandlers: make(map[string]ResourceHandler),
	}, probeErr
}

// SetMenus replaces the native menu bar. Safe to call before or after Run().
func (p *Platform) SetMenus(menus []Menu) {
	p.pendingMenus = menus
	// If the host thread is already running, apply immediately.
	if p.window != 0 {
		mainThread(func() { p.applyMenus(menus) })
	}
}

// MainThread runs f on the AppKit host thread, blocking until it completes.
func (p *Platform) MainThread(f func()) { mainThread(f) }

func (p *Platform) Run() error {
	p.Logger.Log(BackendName, "run_start", nil)
	startAppHost()
	var setupErr error
	mainThread(func() { setupErr = p.setup() })
	if setupErr != nil {
		p.Logger.Log(BackendName, "error", map[string]string{"operation": "setup", "error": debuglog.Error(setupErr)})
		return setupErr
	}
	<-p.runDone
	mainThread(p.endOffscreenActivity)
	p.Logger.Log(BackendName, "closed", nil)
	return nil
}

func (p *Platform) Close() error {
	p.Logger.Log(BackendName, "close_requested", nil)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.runDone)
	p.mu.Unlock()

	// Hide the window so a closed platform doesn't linger on screen while the
	// next window in the same process is running. Read window under lock; the
	// command handler reads it again to guard against the window being
	// destroyed between scheduling and execution.
	mainThread(func() {
		p.mu.Lock()
		w := p.window
		p.window = 0
		p.webview = 0
		p.mu.Unlock()
		if w != 0 {
			w.Send(orderOutSel, 0)
		}
	})
	return nil
}

// InterceptResource registers a resource handler for the given URL scheme.
// Must be called before Run(). scheme is the URL scheme without "://"
// (e.g. "app").
func (p *Platform) InterceptResource(scheme string, handler ResourceHandler) {
	p.schemeHandlers[scheme] = handler
}

// signalExit makes Run() return without closing the window. Callable from the
// host thread (windowWillClose:) or any other thread (Close()). Uses a non-
// blocking channel send so it is safe on the host thread where Close()'s
// mainThread orderOut would deadlock. Sets closed=true so a subsequent Close()
// does not try to orderOut: a window that is already being destroyed.
func (p *Platform) signalExit() {
	p.Logger.Log(BackendName, "close_requested", nil)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.window = 0
	p.webview = 0
	p.mu.Unlock()
	// Do NOT call [NSApp stop:] here — it kills the host thread's run loop,
	// which deadlocks any subsequent mainThread() call (process-global
	// singleton). The run loop lives until the process exits; that's fine.
	select {
	case p.runDone <- struct{}{}:
	default:
	}
}

// applyMenus builds and installs a native NSMenu bar from the given Menu slice.
// Runs on the host thread.
