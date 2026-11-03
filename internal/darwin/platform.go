//go:build darwin

package darwin

// Platform lifecycle and AppKit host-thread dispatch.

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/debuglog"
	"github.com/opentoys/webview/internal/types"
)

var (
	hostDone      = make(chan struct{})
	hostOnce      sync.Once
	hostReady     = make(chan struct{}) // closed once the host loop is running
	hostThread    objc.ID
	commandTarget objc.ID
)

// startAppHost launches the single AppKit host thread if not already running,
// then blocks until it is ready.
func startAppHost() {
	hostOnce.Do(func() {
		go hostLoop()
	})
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
	close(hostDone)
}
func hostLoop() {
	runtime.LockOSThread()
	initializeHost()
	pumpHost()
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
	ownsHost := false
	hostOnce.Do(func() { ownsHost = true; initializeHost() })
	if err := start(); err != nil {
		if ownsHost {
			close(hostDone)
		}
		return err
	}
	if ownsHost {
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
	case commandChan <- func() { fn(); close(done) }:
	case <-hostDone:
		return
	}
	commandTarget.Send(objc.RegisterName("performSelector:onThread:withObject:waitUntilDone:"), objc.RegisterName("runCommand:"), hostThread, objc.ID(0), false)
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

func (p *Platform) ChooseFile(filters ...types.FileFilter) (string, error) {
	return p.choosePath(false, filters...)
}
func (p *Platform) SaveFile(opts types.FileDialogOptions) (string, error) {
	p.mu.Lock()
	ready := p.window != 0 && !p.closed
	p.mu.Unlock()
	if !ready {
		return "", fmt.Errorf("webview: save file: window is not running")
	}
	var path string
	var err error
	mainThread(func() {
		panel := objc.ID(nsSavePanelClass).Send(savePanelSel)
		if panel == 0 {
			err = fmt.Errorf("webview: save file: failed to create NSSavePanel")
			return
		}
		applyFileDialogOptions(panel, opts)
		if panel.Send(runModalSel) != 1 {
			err = types.ErrDialogCancelled
			return
		}
		url := panel.Send(panelURLSel)
		path = goString(url.Send(pathSel))
		if path == "" {
			err = fmt.Errorf("webview: save file: panel returned no filesystem path")
			return
		}
		path, err = filepath.Abs(filepath.Clean(path))
	})
	return path, err
}
func (p *Platform) ChooseDir() (string, error) { return p.choosePath(true) }

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

func (p *Platform) choosePath(directory bool, filters ...types.FileFilter) (string, error) {
	p.mu.Lock()
	ready := p.window != 0 && !p.closed
	p.mu.Unlock()
	if !ready {
		return "", fmt.Errorf("webview: choose path: window is not running")
	}
	var path string
	var err error
	mainThread(func() {
		panel := objc.ID(nsOpenPanelClass).Send(openPanelSel)
		if panel == 0 {
			err = fmt.Errorf("webview: choose path: failed to create NSOpenPanel")
			return
		}
		panel.Send(setCanChooseFilesSel, !directory)
		panel.Send(setCanChooseDirectoriesSel, directory)
		panel.Send(setAllowsMultipleSelectionSel, false)
		if !directory {
			applyFileFilters(panel, filters)
		}
		if panel.Send(runModalSel) != 1 {
			err = types.ErrDialogCancelled
			return
		}
		url := panel.Send(panelURLSel)
		path = goString(url.Send(pathSel))
		if path == "" {
			err = fmt.Errorf("webview: choose path: NSOpenPanel returned no filesystem path")
			return
		}
		path, err = filepath.Abs(path)
	})
	return path, err
}

func (p *Platform) Run() error {
	p.Logger.Log(BackendName, "run_start", nil)
	startAppHost()
	var setupErr error
	mainThread(func() { setupErr = p.setup() })
	if setupErr != nil {
		_ = p.Close()
		mainThread(func() { p.closeExtendedMenus(); p.endOffscreenActivity() })
		p.Logger.Log(BackendName, "error", map[string]string{"operation": "setup", "error": debuglog.Error(setupErr)})
		return setupErr
	}
	<-p.runDone
	mainThread(func() { p.closeExtendedMenus(); p.endOffscreenActivity() })
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
