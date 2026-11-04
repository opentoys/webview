//go:build darwin

package darwin

// Platform lifecycle and AppKit host-thread dispatch.

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/debuglog"
	"github.com/opentoys/webview/internal/types"
)

var (
	hostDone      = make(chan struct{})
	hostOnce      sync.Once
	hostPumpOnce  sync.Once
	hostDoneOnce  sync.Once
	hostRunning   atomic.Bool
	hostReady     = make(chan struct{}) // closed once native UI is initialized
	hostThread    objc.ID
	commandTarget objc.ID
)

// startAppHost ensures native UI is initialized. RunApplication owns the event
// loop; creating an object must never start a second AppKit host thread.
func startAppHost() {
	hostOnce.Do(initializeHost)
	<-hostReady
}

// AppKit must be initialized and pumped on the process main thread. Locking
// the importing goroutine here preserves that thread until application Run.
func init() { runtime.LockOSThread() }

func initializeHost() {
	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	hostThread = objc.ID(nsThreadClass).Send(currentThreadSel)
	app.Send(setActivationPolicySel, activationRegular)
	app.Send(activateIgnoringOtherAppsSel, true)
	app.Send(finishLaunchingSel)
	commandTarget = objc.ID(commandHandlerClass).Send(allocSel).Send(initSel)
	close(hostReady)
}
func pumpHost() {
	objc.ID(nsAppClass).Send(sharedApplicationSel).Send(objc.RegisterName("run"))
	hostDoneOnce.Do(func() { close(hostDone) })
}

// RunApplication runs AppKit on the calling (main) thread. The initializer
// creates pending objects before events are delivered. Test hosts can also
// use this to run their test driver on a worker while AppKit owns main.
func RunApplication(start func() error) error {
	probe()
	if probeErr != nil {
		_ = start()
		return probeErr
	}
	hostOnce.Do(initializeHost)
	ownsLoop := false
	hostPumpOnce.Do(func() {
		ownsLoop = true
		hostRunning.Store(true)
	})
	if err := start(); err != nil {
		if ownsLoop {
			hostDoneOnce.Do(func() { close(hostDone) })
		}
		return err
	}
	if ownsLoop {
		pumpHost()
	} else {
		<-hostDone
	}
	return nil
}

// mainThread runs fn on the AppKit host thread, blocking until it completes.
// Before the host has started (no window exists yet) it is a no-op, which makes
// SetTitle/SetHTML/Navigate/Eval safe to call before Run().
func mainThread(fn func()) {
	select {
	case <-hostDone:
		return
	default:
	}
	select {
	case <-hostReady:
	default:
		return
	}
	if objc.ID(nsThreadClass).Send(currentThreadSel) == hostThread {
		fn()
		return
	}
	done := make(chan struct{})
	select {
	case commandChan <- func() {
		fn()
		close(done)
	}:
	case <-hostDone:
		return
	}
	modes := []objc.ID{
		nsString("NSDefaultRunLoopMode"),
		nsString("NSModalPanelRunLoopMode"),
		nsString("NSEventTrackingRunLoopMode"),
	}
	array := objc.ID(nsArrayClass).Send(arrayWithObjectsCountSel, unsafe.Pointer(&modes[0]), uintptr(len(modes)))
	commandTarget.Send(objc.RegisterName("performSelector:onThread:withObject:waitUntilDone:modes:"), objc.RegisterName("runCommand:"), hostThread, objc.ID(0), false, array)
	runtime.KeepAlive(modes)
	select {
	case <-done:
	case <-hostDone:
	}

}

type Platform struct {
	downloadInstances           map[objc.ID]objc.ID
	acceptMu                    sync.Mutex
	acceptVal                   string
	initialHidden               bool
	closeRequest                func()
	scriptHandler               objc.ID
	trayTarget                  objc.ID
	trayOptions                 *types.SystrayOptions
	contextOptions              types.ContextMenuConfig
	trayActions, contextActions types.MenuActions
	trayNative, contextNative   nativeMenu
	statusItem                  objc.ID
	barTags                     []uintptr

	window           objc.ID
	webview          objc.ID
	dropView         objc.ID // NSView subclass that receives native file drops.
	webviewDragTypes objc.ID // original WKWebView registration, restored on disable.
	delegate         objc.ID
	uiDelegate       objc.ID // WKUIDelegate instance; kept alive for the webview.
	downloadDelegate objc.ID // WKNavigationDelegate+WKDownloadDelegate instance.
	// ucc and scriptHandler are owned by this window.
	ucc objc.ID

	// MessageFunc is called with the string body of each JS postMessage to
	// window.webkit.messageHandlers.webviewBridge.
	MessageFunc   func(string)
	BindingScript func() string
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
	filesDropped func([]string)
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
	snapshot := make([]Menu, len(menus))
	for i, menu := range menus {
		snapshot[i] = menu
		snapshot[i].Items = append([]MenuItem(nil), menu.Items...)
	}
	p.mu.Lock()
	p.pendingMenus = snapshot
	ready := p.window != 0 && !p.closed
	p.mu.Unlock()
	if ready {
		mainThread(func() { p.applyMenus(snapshot) })
	}
}

// MainThread runs f on the AppKit host thread, blocking until it completes.
func (p *Platform) MainThread(f func()) { mainThread(f) }

// SetFilesDropped installs (or removes) this window's native file-drop
// receiver. It is safe before Run: setup applies the pending configuration.
func (p *Platform) SetFilesDropped(fn func([]string)) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("webview: cannot register file drop handler after Close")
	}
	p.filesDropped = fn
	w := p.dropView
	p.mu.Unlock()
	if w != 0 {
		mainThread(func() { p.applyFileDropTarget(w, fn != nil) })
	}
	return nil
}

func (p *Platform) Run() error {
	p.Logger.Log(BackendName, "run_start", nil)
	startAppHost()
	var setupErr error
	mainThread(func() { setupErr = p.setup() })
	if setupErr != nil {
		_ = p.Close()
		mainThread(func() {
			p.closeExtendedMenus()
			p.endOffscreenActivity()
		})
		p.Logger.Log(BackendName, "error", map[string]string{"operation": "setup", "error": debuglog.Error(setupErr)})
		return setupErr
	}
	<-p.runDone
	mainThread(func() {
		p.closeExtendedMenus()
		p.endOffscreenActivity()
	})
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

	mainThread(func() {
		p.closeExtendedMenus()
		for download, delegate := range p.downloadInstances {
			download.Send(setDelegateSel, objc.ID(0))
			pendingDownloads.Delete(downloadKey(download))
			delete(objectPlatforms, delegate)
			delegate.Send(releaseSel)
			download.Send(releaseSel)
			delete(p.downloadInstances, download)
		}
		p.endOffscreenActivity()
		for id, owner := range objectPlatforms {
			if owner == p {
				delete(objectPlatforms, id)
			}
		}
		p.mu.Lock()
		window, view := p.window, p.webview
		p.window, p.webview = 0, 0
		p.mu.Unlock()
		if view != 0 {
			view.Send(setUIDelegateSel, objc.ID(0))
			view.Send(setNavigationDelegateSel, objc.ID(0))
			view.Send(objc.RegisterName("stopLoading"))
		}
		if p.ucc != 0 {
			p.ucc.Send(objc.RegisterName("removeScriptMessageHandlerForName:"), nsString("webviewBridge"))
		}
		if window != 0 {
			window.Send(setDelegateSel, objc.ID(0))
			window.Send(objc.RegisterName("close"))
			window.Send(releaseSel)
		}
		for _, id := range []objc.ID{view, p.dropView, p.delegate, p.uiDelegate, p.downloadDelegate, p.ucc, p.scriptHandler} {
			if id != 0 {
				id.Send(releaseSel)
			}
		}
		p.dropView, p.delegate, p.uiDelegate, p.downloadDelegate, p.ucc, p.scriptHandler = 0, 0, 0, 0, 0, 0
	})
	return nil
}

// InterceptResource registers a resource handler for the given URL scheme.
// Must be called before Run(). scheme is the URL scheme without "://"
// (e.g. "app").
func (p *Platform) InterceptResource(scheme string, handler ResourceHandler) {
	p.schemeHandlers[scheme] = handler
}

// signalExit schedules disposal after a native close notification.
func (p *Platform) signalExit() { go p.Close() }
