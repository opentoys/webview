//go:build linux

// Linux WebView backend in pure Go via purego's C-function bindings.
//
// Reimplements the GTK/WebKitGTK backend by dlopen/dlsym-ing the system GTK
// and WebKitGTK shared objects directly — no cgo needed. Detects the runtime
// stack: GTK4 + webkitgtk-6.0 when present, else GTK3 + webkit2gtk-4.1
// (falling back to -4.0).
//
// Adapted from docs/glaze/webview_linux.go to match this project's Platform
// interface (error returns, EvalHost, pending state, webviewBridge protocol).

package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/opentoys/webview/internal/types"
)

// Re-export shared types from internal/types.
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

const (
	gtkWindowToplevel = 0

	gPriorityHighIdle = 100
	gSourceRemove     = 0

	injectTopFrame        = 1 // WEBKIT_USER_CONTENT_INJECT_TOP_FRAME
	injectAtDocumentStart = 0 // WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START

	gdkHintMaxSize   = 1 << 2 // GDK_HINT_MAX_SIZE
	gSignalMatchData = 1 << 4 // G_SIGNAL_MATCH_DATA

	defaultWidth  = 640
	defaultHeight = 480
)

// gdkGeometry mirrors the C GdkGeometry struct.
type gdkGeometry struct {
	MinWidth, MinHeight   int32
	MaxWidth, MaxHeight   int32
	BaseWidth, BaseHeight int32
	WidthInc, HeightInc   int32
	MinAspect, MaxAspect  float64
	WinGravity            int32
	_                     int32
}

// --- bound C functions -----------------------------------------------------

var (
	gIdleAddFull                     func(priority int, function, data, notify uintptr) uint32
	gMainContextIteration            func(context uintptr, mayBlock bool) bool
	gFree                            func(ptr uintptr)
	gObjectRefSink                   func(obj uintptr) uintptr
	gObjectUnref                     func(obj uintptr)
	gSignalConnectData               func(instance uintptr, signal string, handler, data, destroy uintptr, flags int) uint64
	gSignalHandlersDisconnectMatched func(instance uintptr, mask int, signalID, detail uint32, closure, fn, data uintptr) uint32

	gtkInitCheck              func(argc, argv uintptr) bool
	gtkWindowNew              func(typ int) uintptr
	gtkWindowSetTitle         func(window uintptr, title string)
	gtkWindowSetResizable     func(window uintptr, resizable bool)
	gtkWindowResize           func(window uintptr, w, h int)
	gtkWidgetSetSizeRequest   func(widget uintptr, w, h int)
	gtkWindowSetGeometryHints func(window, widget uintptr, geom *gdkGeometry, mask int)
	gtkContainerAdd           func(container, widget uintptr)
	gtkContainerRemove        func(container, widget uintptr)
	gtkWidgetShow             func(widget uintptr)
	gtkWidgetShowAll          func(widget uintptr)
	gtkWidgetGrabFocus        func(widget uintptr)
	gtkWindowPresent          func(window uintptr)
	gtkWindowClose            func(window uintptr)
	gtkWindowSetPosition      func(window uintptr, position int)

	// GTK 4 variants.
	gtk4                    bool
	gtkInitCheck0           func() bool
	gtkWindowNew0           func() uintptr
	gtkWindowSetChild       func(window, widget uintptr)
	gtkWidgetSetVisible     func(widget uintptr, visible bool)
	gtkWindowSetDefaultSize func(window uintptr, w, h int)
	webkitRegisterHandler3  func(manager uintptr, name string, world uintptr)

	webkitWebViewNew                              func() uintptr
	webkitWebViewGetUserContentManager            func(webview uintptr) uintptr
	webkitWebViewGetSettings                      func(webview uintptr) uintptr
	webkitSettingsSetJavascriptCanAccessClipboard func(settings uintptr, enabled bool)
	webkitSettingsSetEnableWriteConsoleToStdout   func(settings uintptr, enabled bool)
	webkitSettingsSetEnableDeveloperExtras        func(settings uintptr, enabled bool)
	webkitWebViewLoadURI                          func(webview uintptr, uri string)
	webkitWebViewLoadHTML                         func(webview uintptr, html string, baseURI uintptr)
	webkitWebViewGetURI                           func(webview uintptr) uintptr
	webkitUserContentManagerRegisterHandler       func(manager uintptr, name string)
	webkitUserContentManagerAddScript             func(manager, script uintptr)
	webkitUserContentManagerRemoveAllScripts      func(manager uintptr)
	webkitUserScriptNew                           func(source string, frames, time int, allow, block uintptr) uintptr
	webkitUserScriptUnref                         func(script uintptr)
	webkitJavascriptResultGetJSValue              func(result uintptr) uintptr

	webkitWebViewEvaluateJavascript func(webview uintptr, script string, length int, world, source, cancellable, callback, userData uintptr)
	webkitWebViewRunJavascript      func(webview uintptr, script string, cancellable, callback, userData uintptr)
	haveEvaluateJavascript          bool

	jscValueToString func(value uintptr) uintptr
)

// --- one-time init ---------------------------------------------------------

var (
	initOnce     sync.Once
	initErr      error
	uiThreadOnce sync.Once

	dispatchSourceFn uintptr
	messageHandlerFn uintptr
	windowDestroyFn  uintptr

	gtkLib uintptr
)

func openFirst(names ...string) (uintptr, error) {
	var lastErr error
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf("webview: none of %v could be loaded: %w", names, lastErr)
}

func ensureInit() error {
	initOnce.Do(func() {
		glib, err := openFirst("libglib-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}
		gobject, err := openFirst("libgobject-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}

		// Prefer GTK4 + webkitgtk-6.0; fall back to GTK3 + webkit2gtk-4.x.
		// Probe webkit first to avoid loading both GTK3 and GTK4 into one process.
		var gtk, webkit, jsc uintptr
		wk6, werr := openFirst("libwebkitgtk-6.0.so.4")
		if werr == nil {
			gtk4 = true
			webkit = wk6
			gtk, err = openFirst("libgtk-4.so.1")
			if err != nil {
				initErr = err
				return
			}
			jsc, err = openFirst("libjavascriptcoregtk-6.0.so.1")
			if err != nil {
				initErr = err
				return
			}
		} else {
			gtk, err = openFirst("libgtk-3.so.0")
			if err != nil {
				initErr = err
				return
			}
			webkit, err = openFirst("libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37")
			if err != nil {
				initErr = err
				return
			}
			jsc, err = openFirst("libjavascriptcoregtk-4.1.so.0", "libjavascriptcoregtk-4.0.so.18")
			if err != nil {
				initErr = err
				return
			}
		}

		gtkLib = gtk

		purego.RegisterLibFunc(&gIdleAddFull, glib, "g_idle_add_full")
		purego.RegisterLibFunc(&gMainContextIteration, glib, "g_main_context_iteration")
		purego.RegisterLibFunc(&gFree, glib, "g_free")
		purego.RegisterLibFunc(&gObjectRefSink, gobject, "g_object_ref_sink")
		purego.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
		purego.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")
		purego.RegisterLibFunc(&gSignalHandlersDisconnectMatched, gobject, "g_signal_handlers_disconnect_matched")

		if gtk4 {
			purego.RegisterLibFunc(&gtkInitCheck0, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew0, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkWindowSetChild, gtk, "gtk_window_set_child")
			purego.RegisterLibFunc(&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible")
			purego.RegisterLibFunc(&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size")
		} else {
			purego.RegisterLibFunc(&gtkInitCheck, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkContainerAdd, gtk, "gtk_container_add")
			purego.RegisterLibFunc(&gtkContainerRemove, gtk, "gtk_container_remove")
			purego.RegisterLibFunc(&gtkWidgetShow, gtk, "gtk_widget_show")
			purego.RegisterLibFunc(&gtkWidgetShowAll, gtk, "gtk_widget_show_all")
			purego.RegisterLibFunc(&gtkWindowResize, gtk, "gtk_window_resize")
			purego.RegisterLibFunc(&gtkWindowSetGeometryHints, gtk, "gtk_window_set_geometry_hints")
		}
		purego.RegisterLibFunc(&gtkWindowSetTitle, gtk, "gtk_window_set_title")
		purego.RegisterLibFunc(&gtkWindowSetResizable, gtk, "gtk_window_set_resizable")
		purego.RegisterLibFunc(&gtkWidgetSetSizeRequest, gtk, "gtk_widget_set_size_request")
		purego.RegisterLibFunc(&gtkWidgetGrabFocus, gtk, "gtk_widget_grab_focus")
		purego.RegisterLibFunc(&gtkWindowPresent, gtk, "gtk_window_present")
		purego.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")
		purego.RegisterLibFunc(&gtkWindowSetPosition, gtk, "gtk_window_set_position")

		purego.RegisterLibFunc(&webkitWebViewNew, webkit, "webkit_web_view_new")
		purego.RegisterLibFunc(&webkitWebViewGetUserContentManager, webkit, "webkit_web_view_get_user_content_manager")
		purego.RegisterLibFunc(&webkitWebViewGetSettings, webkit, "webkit_web_view_get_settings")
		purego.RegisterLibFunc(&webkitSettingsSetJavascriptCanAccessClipboard, webkit, "webkit_settings_set_javascript_can_access_clipboard")
		purego.RegisterLibFunc(&webkitSettingsSetEnableWriteConsoleToStdout, webkit, "webkit_settings_set_enable_write_console_messages_to_stdout")
		purego.RegisterLibFunc(&webkitSettingsSetEnableDeveloperExtras, webkit, "webkit_settings_set_enable_developer_extras")
		purego.RegisterLibFunc(&webkitWebViewLoadURI, webkit, "webkit_web_view_load_uri")
		purego.RegisterLibFunc(&webkitWebViewLoadHTML, webkit, "webkit_web_view_load_html")
		purego.RegisterLibFunc(&webkitWebViewGetURI, webkit, "webkit_web_view_get_uri")
		purego.RegisterLibFunc(&webkitUserContentManagerAddScript, webkit, "webkit_user_content_manager_add_script")
		purego.RegisterLibFunc(&webkitUserContentManagerRemoveAllScripts, webkit, "webkit_user_content_manager_remove_all_scripts")
		purego.RegisterLibFunc(&webkitUserScriptNew, webkit, "webkit_user_script_new")
		purego.RegisterLibFunc(&webkitUserScriptUnref, webkit, "webkit_user_script_unref")
		if gtk4 {
			purego.RegisterLibFunc(&webkitRegisterHandler3, webkit, "webkit_user_content_manager_register_script_message_handler")
		} else {
			purego.RegisterLibFunc(&webkitUserContentManagerRegisterHandler, webkit, "webkit_user_content_manager_register_script_message_handler")
			purego.RegisterLibFunc(&webkitJavascriptResultGetJSValue, webkit, "webkit_javascript_result_get_js_value")
		}

		_, e := purego.Dlsym(webkit, "webkit_web_view_evaluate_javascript")
		if e == nil {
			purego.RegisterLibFunc(&webkitWebViewEvaluateJavascript, webkit, "webkit_web_view_evaluate_javascript")
			haveEvaluateJavascript = true
		} else {
			purego.RegisterLibFunc(&webkitWebViewRunJavascript, webkit, "webkit_web_view_run_javascript")
		}

		purego.RegisterLibFunc(&jscValueToString, jsc, "jsc_value_to_string")

		dispatchSourceFn = purego.NewCallback(func(data uintptr) uintptr {
			dispatchMu.Lock()
			f := dispatchMap[data]
			delete(dispatchMap, data)
			dispatchMu.Unlock()
			if f != nil {
				f()
			}
			return gSourceRemove
		})
		messageHandlerFn = purego.NewCallback(func(manager, jsResult, userData uintptr) uintptr {
			w := lookupPlatform(userData)
			if w != nil {
				w.onMessage(jsResultToString(jsResult))
			}
			return 0
		})
		windowDestroyFn = purego.NewCallback(func(widget, userData uintptr) uintptr {
			w := lookupPlatform(userData)
			if w != nil {
				w.onWindowDestroy()
			}
			return 0
		})
	})
	return initErr
}

// jsResultToString converts the script-message callback's second argument to
// a Go string. GTK4 delivers a JSCValue* directly; GTK3 wraps it in a
// WebKitJavascriptResult* that must be unwrapped first.
func jsResultToString(arg uintptr) string {
	value := arg
	if !gtk4 {
		value = webkitJavascriptResultGetJSValue(arg)
	}
	cs := jscValueToString(value)
	s := cstr(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

func gtkInit() bool {
	if gtk4 {
		return gtkInitCheck0()
	}
	return gtkInitCheck(0, 0)
}

func gtkNewWindow() uintptr {
	if gtk4 {
		return gtkWindowNew0()
	}
	return gtkWindowNew(gtkWindowToplevel)
}

func registerScriptHandler(manager uintptr, name string) {
	if gtk4 {
		webkitRegisterHandler3(manager, name, 0)
		return
	}
	webkitUserContentManagerRegisterHandler(manager, name)
}

func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	var n int
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n))
}

// --- instance + dispatch registries ----------------------------------------

var (
	regMu      sync.Mutex
	registry   = map[uintptr]*Platform{}
	engineSeq  uintptr

	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

func registerPlatform(p *Platform) uintptr {
	regMu.Lock()
	engineSeq++
	id := engineSeq
	registry[id] = p
	regMu.Unlock()
	return id
}

func unregisterPlatform(id uintptr) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupPlatform(id uintptr) *Platform {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

func dispatchMain(f func()) {
	dispatchMu.Lock()
	dispatchSeq++
	id := dispatchSeq
	dispatchMap[id] = f
	dispatchMu.Unlock()
	gIdleAddFull(gPriorityHighIdle, dispatchSourceFn, id, 0)
}

// --- Platform --------------------------------------------------------------

// Platform implements the webview.Platform interface for Linux using GTK and
// WebKitGTK via purego.
type Platform struct {
	id      uintptr
	window  uintptr
	webview uintptr
	manager uintptr

	ownsWindow    bool
	stopRunLoop   bool
	isWindowShown bool
	isSizeSet     bool

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
}

// New creates a new Platform instance.
func New() *Platform {
	return &Platform{
		schemeHandlers: make(map[string]ResourceHandler),
	}
}

// Run creates the window and enters the GTK main loop. Blocks until Close is
// called or the window is destroyed.
func (p *Platform) Run() error {
	if err := ensureInit(); err != nil {
		return err
	}
	uiThreadOnce.Do(runtime.LockOSThread)

	p.id = registerPlatform(p)
	if err := p.windowInit(0); err != nil {
		unregisterPlatform(p.id)
		return err
	}
	if err := p.registerSchemes(); err != nil {
		p.destroy()
		return err
	}
	p.windowSettings()

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

// Close signals the main loop to stop.
func (p *Platform) Close() error {
	p.stopRunLoop = true
	return nil
}

func (p *Platform) windowInit(window uintptr) error {
	if window != 0 {
		p.window = window
		p.ownsWindow = false
	} else {
		if !gtkInit() {
			return errors.New("webview: gtk_init_check failed (no display?)")
		}
		p.window = gtkNewWindow()
		gtkWindowSetPosition(p.window, 1) // GTK_WIN_POS_CENTER
		gSignalConnectData(p.window, "destroy", windowDestroyFn, p.id, 0, 0)
	}

	p.webview = webkitWebViewNew()
	gObjectRefSink(p.webview)
	p.manager = webkitWebViewGetUserContentManager(p.webview)

	gSignalConnectData(p.manager, "script-message-received::webviewBridge",
		messageHandlerFn, p.id, 0, 0)
	registerScriptHandler(p.manager, "webviewBridge")

	p.pushUserScript(bootstrapJS(nil))
	return nil
}

func (p *Platform) windowSettings() {
	settings := webkitWebViewGetSettings(p.webview)
	webkitSettingsSetJavascriptCanAccessClipboard(settings, true)
	if p.Debug {
		webkitSettingsSetEnableWriteConsoleToStdout(settings, true)
		webkitSettingsSetEnableDeveloperExtras(settings, true)
	}
}

func (p *Platform) onWindowDestroy() {
	unregisterPlatform(p.id)
	p.window = 0
	dispatchMain(func() { p.stopRunLoop = true })
}

func (p *Platform) destroy() {
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
func (p *Platform) applyPending() {
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
func (p *Platform) SetTitle(title string) error {
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
func (p *Platform) SetSize(width, height int, hint SizeHint) {
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

func (p *Platform) applySize(width, height int, hint SizeHint) {
	gtkWindowSetResizable(p.window, hint != SizeFixed)
	switch hint {
	case SizeMin:
		gtkWidgetSetSizeRequest(p.window, width, height)
	case SizeMax:
		if !gtk4 {
			g := gdkGeometry{MaxWidth: int32(width), MaxHeight: int32(height)}
			gtkWindowSetGeometryHints(p.window, 0, &g, gdkHintMaxSize)
		}
	default: // SizeNone, SizeFixed
		if gtk4 {
			gtkWindowSetDefaultSize(p.window, width, height)
		} else {
			gtkWindowResize(p.window, width, height)
		}
	}
	p.windowShow()
}

// Navigate loads the given URL.
func (p *Platform) Navigate(url string) error {
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
func (p *Platform) SetHTML(html string) error {
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
func (p *Platform) Eval(js string) error {
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
func (p *Platform) EvalHost(js string) {
	dispatchMain(func() { p.Eval(js) })
}

// InterceptResource registers a resource handler for the given URL scheme.
// Must be called before Run(). scheme is the URL scheme without "://".
func (p *Platform) InterceptResource(scheme string, handler ResourceHandler) {
	p.schemeHandlers[scheme] = handler
}

func (p *Platform) windowShow() {
	if p.isWindowShown {
		return
	}
	if gtk4 {
		gtkWindowSetChild(p.window, p.webview)
		gtkWidgetSetVisible(p.webview, true)
		gtkWidgetSetVisible(p.window, true)
	} else {
		gtkContainerAdd(p.window, p.webview)
		gtkWidgetShowAll(p.window)
	}
	if p.ownsWindow {
		gtkWidgetGrabFocus(p.webview)
		gtkWindowPresent(p.window)
	}
	p.isWindowShown = true
}

// --- user scripts ----------------------------------------------------------

var userScriptSrcs []string

func (p *Platform) pushUserScript(src string) {
	userScriptSrcs = append(userScriptSrcs, src)
	p.rebuildScripts()
}

func (p *Platform) rebuildScripts() {
	if p.manager == 0 {
		return
	}
	webkitUserContentManagerRemoveAllScripts(p.manager)
	for _, src := range userScriptSrcs {
		addUserScript(p.manager, src)
	}
	// Add bind script for currently bound functions.
	if p.BoundFuncs != nil {
		names := p.BoundFuncs()
		if len(names) > 0 {
			addUserScript(p.manager, bindScript(names))
		}
	}
}

func addUserScript(manager uintptr, src string) {
	script := webkitUserScriptNew(src, injectTopFrame, injectAtDocumentStart, 0, 0)
	webkitUserContentManagerAddScript(manager, script)
	webkitUserScriptUnref(script)
}

// bindScript returns JS that creates window.<name> stubs for each bound func.
func bindScript(names []string) string {
	s := ""
	for _, name := range names {
		lit := marshalJSON(name)
		s += "window[" + lit + "] = function() { return window.webviewBridge.call(" + lit + ", Array.prototype.slice.call(arguments)); };"
	}
	return s
}

// --- message routing -------------------------------------------------------

func (p *Platform) onMessage(body string) {
	if p.MessageFunc != nil {
		p.MessageFunc(body)
	}
}

// --- scheme handling -------------------------------------------------------

// registerSchemes wires each ResourceHandler onto the WebKitWebContext and
// marks the scheme as a secure context. Called from Run before the main loop.
func (p *Platform) registerSchemes() error {
	if len(p.schemeHandlers) == 0 {
		return nil
	}
	if p.webview == 0 {
		return errors.New("webview: register schemes: web view not created")
	}

	glib, err := openFirst("libglib-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load glib: %w", err)
	}
	gobject, err := openFirst("libgobject-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gobject: %w", err)
	}
	gio, err := openFirst("libgio-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gio: %w", err)
	}

	webkitSonames := []string{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37"}
	if gtk4 {
		webkitSonames = []string{"libwebkitgtk-6.0.so.4"}
	}
	webkit, err := openFirst(webkitSonames...)
	if err != nil {
		return fmt.Errorf("webview: register schemes: load webkit: %w", err)
	}

	var (
		getContext               func(uintptr) uintptr
		registerScheme           func(ctx uintptr, scheme string, cb, data, notify uintptr)
		getSecurityManager       func(uintptr) uintptr
		registerAsSecure         func(sm uintptr, scheme string)
		requestGetURI            func(uintptr) uintptr
		requestGetScheme         func(uintptr) uintptr
		schemeRequestFinish      func(req, stream uintptr, streamLen int64, contentType string)
		schemeRequestFinishError func(req, err uintptr)
		memInputStreamNew        func(data unsafe.Pointer, length int, destroy uintptr) uintptr
		schemeGObjectUnref       func(uintptr)
		newErrorLiteral          func(domain uint32, code int32, message string) uintptr
		freeError                func(err uintptr)
		ioErrorQuark             func() uint32
		gFreeAddr                uintptr
	)
	purego.RegisterLibFunc(&getContext, webkit, "webkit_web_view_get_context")
	purego.RegisterLibFunc(&registerScheme, webkit, "webkit_web_context_register_uri_scheme")
	purego.RegisterLibFunc(&getSecurityManager, webkit, "webkit_web_context_get_security_manager")
	purego.RegisterLibFunc(&registerAsSecure, webkit, "webkit_security_manager_register_uri_scheme_as_secure")
	purego.RegisterLibFunc(&requestGetURI, webkit, "webkit_uri_scheme_request_get_uri")
	purego.RegisterLibFunc(&requestGetScheme, webkit, "webkit_uri_scheme_request_get_scheme")
	purego.RegisterLibFunc(&schemeRequestFinish, webkit, "webkit_uri_scheme_request_finish")
	purego.RegisterLibFunc(&schemeRequestFinishError, webkit, "webkit_uri_scheme_request_finish_error")
	purego.RegisterLibFunc(&memInputStreamNew, gio, "g_memory_input_stream_new_from_data")
	purego.RegisterLibFunc(&schemeGObjectUnref, gobject, "g_object_unref")
	purego.RegisterLibFunc(&newErrorLiteral, glib, "g_error_new_literal")
	purego.RegisterLibFunc(&freeError, glib, "g_error_free")
	purego.RegisterLibFunc(&ioErrorQuark, gio, "g_io_error_quark")

	gFreeAddr, err = purego.Dlsym(glib, "g_free")
	if err != nil {
		return fmt.Errorf("webview: register schemes: resolve g_free: %w", err)
	}

	memdup, err := resolveMemdup(glib)
	if err != nil {
		return fmt.Errorf("webview: register schemes: %w", err)
	}

	ctx := getContext(p.webview)
	if ctx == 0 {
		return errors.New("webview: register schemes: web context is nil")
	}
	sm := getSecurityManager(ctx)
	if sm == 0 {
		return errors.New("webview: register schemes: security manager is nil")
	}

	p.schemeCB = purego.NewCallback(func(request uintptr, data uintptr) uintptr {
		eng := lookupPlatform(data)
		if eng == nil {
			return 0
		}
		url := cstr(requestGetURI(request))
		scheme := cstr(requestGetScheme(request))

		eng.mu.Lock()
		handler := eng.schemeHandlers[scheme]
		eng.mu.Unlock()
		if handler == nil {
			const gIOErrorNotFound = 1
			gerr := newErrorLiteral(ioErrorQuark(), gIOErrorNotFound, "resource not found")
			schemeRequestFinishError(request, gerr)
			freeError(gerr)
			return 0
		}

		sr := ResourceRequest{URL: url}
		var resp *ResourceResponse
		handler(sr, func(r *ResourceResponse) {
			resp = r
		})
		if resp == nil {
			const gIOErrorNotFound = 1
			gerr := newErrorLiteral(ioErrorQuark(), gIOErrorNotFound, "resource not found")
			schemeRequestFinishError(request, gerr)
			freeError(gerr)
			return 0
		}

		mime := "application/octet-stream"
		if ct, ok := resp.Headers["Content-Type"]; ok {
			mime = ct
		} else if ct, ok := resp.Headers["content-type"]; ok {
			mime = ct
		}

		body := resp.Body
		var dataPtr unsafe.Pointer
		if len(body) > 0 {
			dataPtr = memdup(unsafe.Pointer(&body[0]), len(body))
		}
		stream := memInputStreamNew(dataPtr, len(body), uintptr(gFreeAddr))
		schemeRequestFinish(request, stream, int64(len(body)), mime)
		schemeGObjectUnref(stream)
		return 0
	})

	for scheme := range p.schemeHandlers {
		registerScheme(ctx, scheme, p.schemeCB, p.id, 0)
		registerAsSecure(sm, scheme)
	}
	return nil
}

// resolveMemdup returns g_memdup2 (GLib >= 2.68) or g_memdup (older).
func resolveMemdup(glib uintptr) (func(mem unsafe.Pointer, size int) unsafe.Pointer, error) {
	addr, err := purego.Dlsym(glib, "g_memdup2")
	if err == nil && addr != 0 {
		var f func(mem unsafe.Pointer, size uint64) unsafe.Pointer
		purego.RegisterFunc(&f, addr)
		return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, uint64(size)) }, nil
	}
	addr, err = purego.Dlsym(glib, "g_memdup")
	if err == nil && addr != 0 {
		var f func(mem unsafe.Pointer, size uint32) unsafe.Pointer
		purego.RegisterFunc(&f, addr)
		return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, uint32(size)) }, nil
	}
	return nil, errors.New("neither g_memdup2 nor g_memdup is available")
}

// --- helpers ---------------------------------------------------------------

func marshalJSON(msg string) string {
	data, _ := json.Marshal(msg)
	return string(data)
}
