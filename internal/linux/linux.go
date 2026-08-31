//go:build linux

// Linux WebView backend in pure Go via purego C-function bindings.
// Stack: GTK4 + WebKitGTK 6.0.
package linux

import (
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/opentoys/webview/internal/types"
)

// Re-export shared types from types.
type SizeHint = types.SizeHint

const (
	SizeNone  = types.SizeNone
	SizeMin   = types.SizeMin
	SizeMax   = types.SizeMax
	SizeFixed = types.SizeFixed
)

type ResourceRequest = types.ResourceRequest
type ResourceResponse = types.ResourceResponse
type ResourceHandler = types.ResourceHandler
type Menu = types.Menu
type MenuItem = types.MenuItem

// --- Platform --------------------------------------------------------------

// Gtk is the public surface implemented by the shared linux backend (and,
// via embedding, by the GTK variants). It mirrors webview.Platform.
type Gtk interface {
	Run() error
	Close() error
	SetTitle(title string) error
	SetSize(w, h int, hint SizeHint)
	Navigate(url string) error
	SetHTML(html string) error
	Eval(js string) error
	Init(js string) error
	InterceptResource(scheme string, handler ResourceHandler)
	SetMenus(menus []Menu)
	MainThread(f func())
}

// gtk is the shared Linux GTK/WebKitGTK backend. The GTK4 adapters supply the
// version-specific hooks through the function fields below.
type gtk struct {
	id      uintptr
	window  uintptr
	webview uintptr
	manager uintptr

	ownsWindow    bool
	stopRunLoop   bool
	isWindowShown bool
	isSizeSet     bool
	menuBar       uintptr

	// Version-specific hooks, set by the gtk4 constructor.
	createWindowFn          func(*gtk) error
	buildMenubarFn          func(*gtk, []Menu)
	showWindowFn            func(*gtk)
	applySizeHintFn         func(*gtk, int, int, SizeHint)
	savePathFn              func(*gtk, uintptr) string
	openFilesFn             func(*gtk, uintptr) []string
	messageValueFn          func(*gtk, uintptr) string
	registerScriptHandlerFn func(*gtk, uintptr, string)

	// Callback wiring (set by buildPlatform before Run).
	MessageFunc func(string)
	BoundFuncs  func() []string

	// Options.
	Debug     bool
	Incognito bool
	DataDir   string

	// Pending state applied in setup when window/webview are created.
	mu           sync.Mutex
	pendingTitle string
	pendingHTML  string
	pendingURL   string
	pendingW     int
	pendingH     int
	pendingHint  SizeHint

	// Scheme handlers for InterceptResource.
	schemeHandlers map[string]ResourceHandler
	schemeCB       uintptr

	pendingMenus   []Menu
	hasCustomMenus bool
}

// New creates a new GTK backend instance. The GTK implementation is selected
// in Run once the libraries are loaded.
func New() (*gtk, error) {
	return &gtk{
		schemeHandlers: make(map[string]ResourceHandler),
	}, ensureInit()
}

// selectBackend wires the GTK4 hooks into the shared backend.
func (p *gtk) selectBackend() error {
	return newgtk4(p)
}

// newgtk4 wires the GTK4 implementations into the shared backend.
func newgtk4(p *gtk) error {
	newgtk4symbols()
	p.createWindowFn = pgtk4CreateWindow
	p.buildMenubarFn = pgtk4BuildMenubar
	p.showWindowFn = pgtk4ShowWindow
	p.applySizeHintFn = pgtk4ApplySizeHint
	p.savePathFn = pgtk4SavePath
	p.openFilesFn = pgtk4OpenFiles
	p.messageValueFn = pgtk4MessageValue
	p.registerScriptHandlerFn = pgtk4RegisterScriptHandler
	return nil
}

// Run creates the window and enters the GTK main loop. Blocks until Close is
// called or the window is destroyed.
func (p *gtk) Run() error {

	uiThreadOnce.Do(runtime.LockOSThread)

	p.id = registerPlatform(p)
	if err := p.selectBackend(); err != nil {
		unregisterPlatform(p.id)
		return err
	}
	if err := p.windowInit(0); err != nil {
		unregisterPlatform(p.id)
		return err
	}
	if err := p.registerSchemes(); err != nil {
		p.destroy()
		return err
	}
	p.windowSettings()
	// The default Edit menu is installed by buildPlatform via SetMenus; custom
	// apps append/replace through SetMenu before Run.
	if p.hasCustomMenus {
		p.applyMenus(p.pendingMenus)
	}
	// Apply pending title/size/HTML/URL directly (not via dispatch) so the
	// window is visible before the main loop starts.
	p.applyPending()
	if !p.isSizeSet {
		p.applySize(defaultWidth, defaultHeight, SizeNone)
	}

	// Run the GTK main loop.
	p.stopRunLoop = false
	for !p.stopRunLoop {
		gMainContextIteration(0, true)
	}
	return nil
}

// SetMenus replaces the native menu bar. Call before Run().
func (p *gtk) SetMenus(menus []Menu) {
	p.pendingMenus = menus
	p.hasCustomMenus = len(menus) > 0
}

// MainThread runs f on the GTK main thread, blocking until it completes.
func (p *gtk) MainThread(f func()) {
	done := make(chan struct{})
	dispatchMain(func() {
		f()
		close(done)
	})
	<-done
}

// Close destroys the window and signals the main loop to stop.
func (p *gtk) Close() error {
	if p.window != 0 {
		dispatchMain(func() { gtkWindowClose(p.window) })
	} else {
		p.stopRunLoop = true
	}
	return nil
}

// applyMenus builds and installs a menu bar from the given Menu slice. The
// GTK menubar implementation.
func (p *gtk) applyMenus(menus []Menu) {
	p.buildMenubarFn(p, menus)
}

func (p *gtk) windowInit(window uintptr) error {
	if window != 0 {
		p.window = window
		p.ownsWindow = false
	} else {
		if err := p.createWindowFn(p); err != nil {
			return err
		}
	}
	gSignalConnectData(p.window, "destroy", windowDestroyFn, p.id, 0, 0)

	p.webview = webkitWebViewNew()
	gObjectRefSink(p.webview)
	p.manager = webkitWebViewGetUserContentManager(p.webview)

	gSignalConnectData(p.manager, "script-message-received::webviewBridge",
		messageHandlerFn, p.id, 0, 0)
	p.registerScriptHandlerFn(p, p.manager, "webviewBridge")

	// Connect download-started on the network session — this is the canonical
	// entry point for all downloads on modern WebKitGTK builds (catches both
	// navigation-type <a download> and response-type downloads). We do NOT use
	// decide-policy: it fires for every navigation request and calling
	// get_response() on a WebKitNavigationPolicyDecision crashes with a GLib
	// assertion. download-started only fires for actual downloads.
	fmt.Fprintf(os.Stderr, "webview: connect download hooks\n")
	if webkitNetworkSessionGetDefault != nil {
		session := webkitNetworkSessionGetDefault()
		fmt.Fprintf(os.Stderr, "webview: download session=%x\n", session)
		if session != 0 {
			gSignalConnectData(session, "download-started", downloadStartedFn(), p.id, 0, 0)
			fmt.Fprintf(os.Stderr, "webview: connected download-started on session\n")
		}
	}

	// <input type=file>: show a native open dialog honoring accept.
	gSignalConnectData(p.webview, "run-file-chooser", runFileChooserFn(), p.id, 0, 0)

	p.pushUserScript(bootstrapJS(nil))
	return nil
}

func (p *gtk) windowSettings() {
	settings := webkitWebViewGetSettings(p.webview)
	webkitSettingsSetJavascriptCanAccessClipboard(settings, true)
	if p.Debug {
		webkitSettingsSetEnableWriteConsoleToStdout(settings, true)
		webkitSettingsSetEnableDeveloperExtras(settings, true)
	}
}

func (p *gtk) onWindowDestroy() {
	unregisterPlatform(p.id)
	p.window = 0
	dispatchMain(func() { p.stopRunLoop = true })
}

func (p *gtk) destroy() {
	if p.window != 0 && p.ownsWindow {
		gSignalHandlersDisconnectMatched(p.window, gSignalMatchData, 0, 0, 0, 0, p.id)
		gtkWindowClose(p.window)
		p.window = 0
	}
	if p.webview != 0 {
		if p.manager != 0 {
			gSignalHandlersDisconnectMatched(p.manager, gSignalMatchData, 0, 0, 0, 0, p.id)
			p.manager = 0
		}
		gObjectUnref(p.webview)
		p.webview = 0
	}
	unregisterPlatform(p.id)
}

// applyPending applies title/size/HTML/URL set before Run.
func (p *gtk) applyPending() {
	p.mu.Lock()
	title := p.pendingTitle
	html := p.pendingHTML
	url := p.pendingURL
	pw, ph, phint := p.pendingW, p.pendingH, p.pendingHint
	p.pendingTitle = ""
	p.pendingHTML = ""
	p.pendingURL = ""
	p.pendingW = 0
	p.pendingH = 0
	p.mu.Unlock()

	if pw > 0 && ph > 0 {
		p.applySize(pw, ph, phint)
	}
	if title != "" {
		p.SetTitle(title)
	}
	if html != "" {
		p.SetHTML(html)
	} else if url != "" {
		p.Navigate(url)
	}
}

// SetTitle updates the window title.
func (p *gtk) SetTitle(title string) error {
	p.mu.Lock()
	if p.window == 0 {
		p.pendingTitle = title
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	gtkWindowSetTitle(p.window, title)
	return nil
}

// SetSize updates the window size with the given hint.
func (p *gtk) SetSize(width, height int, hint SizeHint) {
	p.mu.Lock()
	if p.window == 0 {
		p.pendingW = width
		p.pendingH = height
		p.pendingHint = hint
		p.isSizeSet = true
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.applySize(width, height, hint)
}

func (p *gtk) applySize(width, height int, hint SizeHint) {
	gtkWindowSetResizable(p.window, hint != SizeFixed)
	p.applySizeHintFn(p, width, height, hint)
	p.windowShow()
}

// Navigate loads the given URL.
func (p *gtk) Navigate(url string) error {
	p.mu.Lock()
	if p.webview == 0 {
		p.pendingURL = url
		p.pendingHTML = ""
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	if url == "" {
		url = "about:blank"
	}
	webkitWebViewLoadURI(p.webview, url)
	return nil
}

// SetHTML loads HTML content directly.
func (p *gtk) SetHTML(html string) error {
	p.mu.Lock()
	if p.webview == 0 {
		p.pendingHTML = html
		p.pendingURL = ""
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	webkitWebViewLoadHTML(p.webview, html, 0)
	return nil
}

// Eval evaluates JavaScript in the webview.
func (p *gtk) Eval(js string) error {
	if p.webview == 0 {
		return nil
	}
	if webkitWebViewGetURI(p.webview) == 0 {
		return nil
	}
	if haveEvaluateJavascript {
		webkitWebViewEvaluateJavascript(p.webview, js, len(js), 0, 0, 0, 0, 0)
	} else {
		webkitWebViewRunJavascript(p.webview, js, 0, 0, 0)
	}
	return nil
}

// EvalHost queues JS to run on the GTK main thread without blocking. Safe from
// any goroutine.
func (p *gtk) EvalHost(js string) {
	dispatchMain(func() { p.Eval(js) })
}

// Init injects JavaScript that runs at the start of every new page load.
func (p *gtk) Init(js string) error {
	p.pushUserScript(js)
	return nil
}

// InterceptResource registers a resource handler for the given URL scheme.
// Must be called before Run(). scheme is the URL scheme without "://".
func (p *gtk) InterceptResource(scheme string, handler ResourceHandler) {
	p.schemeHandlers[scheme] = handler
}

func (p *gtk) windowShow() {
	if p.isWindowShown {
		return
	}
	p.showWindowFn(p)
	if p.ownsWindow {
		gtkWidgetGrabFocus(p.webview)
		gtkWindowPresent(p.window)
	}
	p.isWindowShown = true
}

func pgtk4CreateWindow(p *gtk) error {
	if !gtkInitCheck4() {
		return errNoDisplay
	}
	p.window = gtkWindowNew4()
	return nil
}

func pgtk4ShowWindow(p *gtk) {
	box := gtkBoxNew(gtkOrientationVertical, 0)
	if p.menuBar != 0 {
		gtkBoxAppend(box, p.menuBar)
		applyMenubarBorderFix()
	}
	// gtk_box_append defaults to no expansion, so force the webview to fill
	// the window instead of collapsing to its minimum size.
	gtkWidgetSetHExpand(p.webview, true)
	gtkWidgetSetVExpand(p.webview, true)
	gtkBoxAppend(box, p.webview)
	gtkWindowSetChild(p.window, box)
	gtkWidgetSetVisible(p.webview, true)
	gtkWidgetSetVisible(p.window, true)
}

func pgtk4ApplySizeHint(p *gtk, width, height int, hint SizeHint) {
	if hint == SizeMax {
		// GTK4 dropped per-axis geometry hints; only default size remains.
		return
	}
	// SizeMin, SizeNone and SizeFixed all map to default size on GTK4.
	gtkWindowSetDefaultSize(p.window, width, height)
}
