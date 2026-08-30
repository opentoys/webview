//go:build linux

// Linux WebView backend in pure Go via purego's C-function bindings.
//
// stack: GTK4 + webkitgtk-6.0

package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
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

const (
	gPriorityHighIdle = 100
	gPriorityInIdle   = -100
	gSourceRemove     = 0

	injectTopFrame        = 1 // WEBKIT_USER_CONTENT_INJECT_TOP_FRAME
	injectAtDocumentStart = 0 // WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START

	gSignalMatchData = 1 << 4 // G_SIGNAL_MATCH_DATA

	defaultWidth  = 640
	defaultHeight = 480

	// GTK box orientation (gtk_box_new).
	gtkOrientationVertical = 1 // GTK_ORIENTATION_VERTICAL
	gSignalActivate        = "activate"
)

// --- bound C functions -----------------------------------------------------

var (
	gIdleAddFull                     func(priority int, function, data, notify uintptr) uint32
	gMainContextIteration            func(context uintptr, mayBlock bool) bool
	gFree                            func(ptr uintptr)
	gObjectRefSink                   func(obj uintptr) uintptr
	gObjectRef                       func(obj uintptr) uintptr
	gObjectUnref                     func(obj uintptr)
	gSignalConnectData               func(instance uintptr, signal string, handler, data, destroy uintptr, flags int) uint64
	gSignalHandlersDisconnectMatched func(instance uintptr, mask int, signalID, detail uint32, closure, fn, data uintptr) uint32

	gtkInitCheck4           func() bool
	gtkWindowNew4           func() uintptr
	gtkWindowSetChild       func(window, widget uintptr)
	gtkWidgetSetVisible     func(widget uintptr, visible bool)
	gtkWidgetSetHExpand     func(widget uintptr, expand bool)
	gtkWidgetSetVExpand     func(widget uintptr, expand bool)
	gtkWindowSetTitle       func(window uintptr, title string)
	gtkWindowSetResizable   func(window uintptr, resizable bool)
	gtkWindowSetDefaultSize func(window uintptr, w, h int)
	gtkWidgetGrabFocus      func(widget uintptr)
	gtkWindowPresent        func(window uintptr)
	gtkWindowClose          func(window uintptr)
	// GTK file chooser (native save dialog for downloads).
	gtkFileChooserNativeNew      func(title string, parent uintptr, action int, accept, cancel string) uintptr
	gtkNativeDialogShow          func(dialog uintptr)
	gtkNativeDialogHide          func(dialog uintptr)
	gtkNativeDialogSetModal      func(dialog uintptr, modal bool)
	gtkNativeDialogRun           func(dialog uintptr) int
	gtkFileChooserSetCurrentName func(chooser uintptr, name string)
	gtkFileChooserGetFile        func(chooser uintptr) uintptr // GFile*
	gtkFileChooserGetFiles       func(chooser uintptr) uintptr // GList* of GFile* (GTK4)
	gFileGetPath                 func(file uintptr) uintptr    // char*
	// File chooser (native open dialog for <input type=file>) + filters.
	gtkFileChooserSetSelectMultiple           func(chooser uintptr, selectMultiple bool)
	gtkFileChooserAddFilter                   func(chooser, filter uintptr)
	gtkFileFilterNew                          func() uintptr
	gtkFileFilterSetName                      func(filter uintptr, name string)
	gtkFileFilterAddMimeType                  func(filter uintptr, mimeType string)
	gtkFileFilterAddPattern                   func(filter uintptr, pattern string)
	webkitFileChooserRequestGetSelectMultiple func(request uintptr) bool
	webkitFileChooserRequestSelectFiles       func(request, files uintptr)
	webkitFileChooserRequestCancel            func(request uintptr)
	gFileNewForPath                           func(path string) uintptr // GFile*
	gListAppend                               func(list, data uintptr) uintptr
	gListLength                               func(list uintptr) uintptr
	gListNthData                              func(list uintptr, n uint) uintptr
	gListFree                                 func(list uintptr)
	gtkBoxNew                                 func(orientation int, spacing int) uintptr
	gtkBoxAppend                              func(box, child uintptr)
	gtkWidgetInsertActionGroup                func(widget uintptr, groupName string, group uintptr)

	// GMenu / GAction for GTK4 menubar.
	gMenuNew                      func() uintptr
	gMenuAppend                   func(menu uintptr, label, detailedAction string)
	gMenuAppendSubmenu            func(menu uintptr, label string, submenu uintptr)
	gMenuAppendSection            func(menu uintptr, label string, section uintptr)
	gSimpleActionNew              func(name string, parameterType uintptr) uintptr
	gSimpleActionGroupNew         func() uintptr
	gSimpleActionGroupInsert      func(group, action uintptr)
	gtkPopoverMenuBarNewFromModel func(model uintptr) uintptr

	// GTK4 CSS API for menubar border removal.
	gtkCssProviderNew          func() uintptr
	gtkCssProviderLoadFromData func(provider uintptr, data uintptr, length int64, err uintptr) bool
	gtkStyleContextAddClass    func(context uintptr, class_name uintptr)
	gtkWidgetGetStyleContext   func(widget uintptr) uintptr
	// GDK display + style provider (for compositor-aware border fix).
	gdkDisplayGetDefault                 func() uintptr
	gdkDisplayIsComposited               func(display uintptr) bool
	gtkStyleContextAddProviderForDisplay func(display, provider uintptr, priority int)
	gtkStyleProviderPriorityApplication  int = 600 // GTK_STYLE_PROVIDER_PRIORITY_APPLICATION

	// WebKitGTK URI request header accessor + libsoup foreach (set in
	// registerSchemes; libsoup 2 vs 3 differ in foreach callback signature).
	requestGetHTTPHeaders     func(uintptr) uintptr
	soupMessageHeadersForeach func(hdrs, cb, userData uintptr)

	// GTK variants.

	webkitWebViewNew                              func() uintptr
	webkitWebViewGetUserContentManager            func(webview uintptr) uintptr
	webkitWebViewGetSettings                      func(webview uintptr) uintptr
	webkitSettingsSetJavascriptCanAccessClipboard func(settings uintptr, enabled bool)
	webkitSettingsSetEnableWriteConsoleToStdout   func(settings uintptr, enabled bool)
	webkitSettingsSetEnableDeveloperExtras        func(settings uintptr, enabled bool)
	webkitWebViewLoadURI                          func(webview uintptr, uri string)
	webkitWebViewLoadHTML                         func(webview uintptr, html string, baseURI uintptr)
	webkitWebViewGetURI                           func(webview uintptr) uintptr
	webkitRegisterHandler3                        func(manager uintptr, name string, world uintptr)
	webkitUserContentManagerAddScript             func(manager, script uintptr)
	webkitUserContentManagerRemoveAllScripts      func(manager uintptr)
	webkitUserScriptNew                           func(source string, frames, time int, allow, block uintptr) uintptr
	webkitUserScriptUnref                         func(script uintptr)

	webkitWebViewEvaluateJavascript func(webview uintptr, script string, length int, world, source, cancellable, callback, userData uintptr)
	webkitWebViewRunJavascript      func(webview uintptr, script string, cancellable, callback, userData uintptr)

	// Download via the modern, always-exported API: a network-session
	// "download-started" callback plus each download's "decide-destination"
	// signal (which already hands us the suggested filename).
	webkitWebViewGetContext        func(webview uintptr) uintptr
	webkitNetworkSessionGetDefault func() uintptr
	haveEvaluateJavascript         bool

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

	// Loaded library handles, valid after ensureInit.
	libGLib    uintptr
	libGObject uintptr
	libGIO     uintptr
	libGTK     uintptr
	libWebKit  uintptr
	libGDK     uintptr

	gtkLib uintptr // alias kept for existing references
)

// errNoDisplay is returned when GTK cannot connect to a display server.
var errNoDisplay = errors.New("webview: gtk_init_check failed (no display?)")

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

// Probe checks whether the native GTK/WebKit environment is available. It is
// safe to call multiple times; the underlying library load happens once.
func Probe() error {
	return ensureInit()
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
		gio, err := openFirst("libgio-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}

		// GTK4 + webkitgtk-6.0 only; GTK3 is not supported.
		gtk, err := openFirst("libgtk-4.so.1")
		if err != nil {
			initErr = err
			return
		}
		webkit, err := openFirst("libwebkitgtk-6.0.so.4")
		if err != nil {
			initErr = err
			return
		}
		jsc, err := openFirst("libjavascriptcoregtk-6.0.so.1")
		if err != nil {
			initErr = err
			return
		}

		gtkLib = gtk
		libGLib, libGObject, libGIO = glib, gobject, gio

		registerShared(glib, gobject, gio, webkit, gtk)

		newgtk4symbols()

		connectDownloadCallbacks()
		connectDialogCallback()

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
				w.onMessage(w.messageValueFn(w, jsResult))
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

// registerShared binds all library symbols that are version-independent (GLib,
// GIO, GObject, WebKit2GTK, JSCore). The two download capability flags are
// probed here since they depend only on WebKit symbols present in every build.
// Per-GTK-version symbols are registered by the backend's registerSymbols().
func registerShared(glib, gobject, gio, webkit, gtk uintptr) {
	purego.RegisterLibFunc(&gIdleAddFull, glib, "g_idle_add_full")
	purego.RegisterLibFunc(&gMainContextIteration, glib, "g_main_context_iteration")
	purego.RegisterLibFunc(&gFree, glib, "g_free")
	purego.RegisterLibFunc(&gMenuNew, gio, "g_menu_new")
	purego.RegisterLibFunc(&gMenuAppend, gio, "g_menu_append")
	purego.RegisterLibFunc(&gMenuAppendSubmenu, gio, "g_menu_append_submenu")
	purego.RegisterLibFunc(&gMenuAppendSection, gio, "g_menu_append_section")
	purego.RegisterLibFunc(&gSimpleActionNew, gio, "g_simple_action_new")
	purego.RegisterLibFunc(&gSimpleActionGroupNew, gio, "g_simple_action_group_new")
	purego.RegisterLibFunc(&gSimpleActionGroupInsert, gio, "g_simple_action_group_insert")
	purego.RegisterLibFunc(&gObjectRefSink, gobject, "g_object_ref_sink")
	purego.RegisterLibFunc(&gObjectRef, gobject, "g_object_ref")
	purego.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
	purego.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")
	purego.RegisterLibFunc(&gSignalHandlersDisconnectMatched, gobject, "g_signal_handlers_disconnect_matched")

	// GTK shared symbols.
	purego.RegisterLibFunc(&gtkWindowSetTitle, gtk, "gtk_window_set_title")
	purego.RegisterLibFunc(&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size")
	purego.RegisterLibFunc(&gtkWindowSetResizable, gtk, "gtk_window_set_resizable")
	purego.RegisterLibFunc(&gtkWidgetGrabFocus, gtk, "gtk_widget_grab_focus")
	purego.RegisterLibFunc(&gtkWindowPresent, gtk, "gtk_window_present")
	purego.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")
	purego.RegisterLibFunc(&gtkFileChooserNativeNew, gtk, "gtk_file_chooser_native_new")
	purego.RegisterLibFunc(&gtkNativeDialogShow, gtk, "gtk_native_dialog_show")
	purego.RegisterLibFunc(&gtkNativeDialogHide, gtk, "gtk_native_dialog_hide")
	purego.RegisterLibFunc(&gtkNativeDialogSetModal, gtk, "gtk_native_dialog_set_modal")
	purego.RegisterLibFunc(&gtkFileChooserSetCurrentName, gtk, "gtk_file_chooser_set_current_name")
	// gtk_native_dialog_run was added in GTK 3.90 / 4.0; some stripped or older
	// builds omit it. Probe with Dlsym so we can fall back to the manual nested
	// iteration path below.
	if _, err := purego.Dlsym(gtk, "gtk_native_dialog_run"); err == nil {
		purego.RegisterLibFunc(&gtkNativeDialogRun, gtk, "gtk_native_dialog_run")
	}
	purego.RegisterLibFunc(&gtkFileChooserGetFile, gtk, "gtk_file_chooser_get_file")
	purego.RegisterLibFunc(&gtkFileChooserGetFiles, gtk, "gtk_file_chooser_get_files")
	// g_file_get_path lives in libgio (GTK4 save-path result).
	purego.RegisterLibFunc(&gFileGetPath, gio, "g_file_get_path")
	// <input type=file> open dialog + accept filters.
	purego.RegisterLibFunc(&gtkFileChooserSetSelectMultiple, gtk, "gtk_file_chooser_set_select_multiple")
	purego.RegisterLibFunc(&gtkFileChooserAddFilter, gtk, "gtk_file_chooser_add_filter")
	purego.RegisterLibFunc(&gtkFileFilterNew, gtk, "gtk_file_filter_new")
	purego.RegisterLibFunc(&gtkFileFilterSetName, gtk, "gtk_file_filter_set_name")
	purego.RegisterLibFunc(&gtkFileFilterAddMimeType, gtk, "gtk_file_filter_add_mime_type")
	purego.RegisterLibFunc(&gtkFileFilterAddPattern, gtk, "gtk_file_filter_add_pattern")
	purego.RegisterLibFunc(&webkitFileChooserRequestGetSelectMultiple, webkit, "webkit_file_chooser_request_get_select_multiple")
	purego.RegisterLibFunc(&webkitFileChooserRequestSelectFiles, webkit, "webkit_file_chooser_request_select_files")
	purego.RegisterLibFunc(&webkitFileChooserRequestCancel, webkit, "webkit_file_chooser_request_cancel")
	purego.RegisterLibFunc(&gFileNewForPath, gio, "g_file_new_for_path")
	purego.RegisterLibFunc(&gListAppend, glib, "g_list_append")
	purego.RegisterLibFunc(&gListLength, glib, "g_list_length")
	purego.RegisterLibFunc(&gListNthData, glib, "g_list_nth_data")
	purego.RegisterLibFunc(&gListFree, glib, "g_list_free")

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
	purego.RegisterLibFunc(&webkitWebViewGetContext, webkit, "webkit_web_view_get_context")
	purego.RegisterLibFunc(&webkitNetworkSessionGetDefault, webkit, "webkit_network_session_get_default")

	_, e := purego.Dlsym(webkit, "webkit_web_view_evaluate_javascript")
	if e == nil {
		purego.RegisterLibFunc(&webkitWebViewEvaluateJavascript, webkit, "webkit_web_view_evaluate_javascript")
		haveEvaluateJavascript = true
	} else {
		purego.RegisterLibFunc(&webkitWebViewRunJavascript, webkit, "webkit_web_view_run_javascript")
	}

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
	regMu     sync.Mutex
	registry  = map[uintptr]*gtk{}
	engineSeq uintptr

	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

func registerPlatform(p *gtk) uintptr {
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

func lookupPlatform(id uintptr) *gtk {
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

// gtk is the shared Linux GTK/WebKitGTK backend.
// (gtk4_linux.go) embed it and supply the version-specific hooks through the
// func fields below, so shared methods call p.xxxFn() and always reach the
// variant that constructed them (function fields avoid Go's embedding "this is
// fixed at the base" pitfall without an interface indirection per call).
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

// New creates a new GTK backend instance.
// selected in Run() once the libraries are loaded.
func New() (*gtk, error) {
	return &gtk{
		schemeHandlers: make(map[string]ResourceHandler),
	}, ensureInit()
}

// selectBackend wires the GTK variant hooks into this shared backend.
// and wires its version-specific hooks into this shared backend.
func (p *gtk) selectBackend() error {
	return newgtk4(p)
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

// soup3 is true when the loaded WebKitGTK links libsoup 3
// (webkitgtk-6.0). Detected once in registerSchemes.
var soup3 bool

// soupForeachCB is the pre-built purego callback for soup_message_headers_foreach,
// chosen by soup3. Set once in registerSchemes.
var soupForeachCB uintptr

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

// --- user scripts ----------------------------------------------------------

var userScriptSrcs []string

func (p *gtk) pushUserScript(src string) {
	userScriptSrcs = append(userScriptSrcs, src)
	p.rebuildScripts()
}

func (p *gtk) rebuildScripts() {
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

func (p *gtk) onMessage(body string) {
	if p.MessageFunc != nil {
		p.MessageFunc(body)
	}
}

// --- scheme handling -------------------------------------------------------

// registerSchemes wires each ResourceHandler onto the WebKitWebContext and
// marks the scheme as a secure context. Called from Run before the main loop.
func (p *gtk) registerSchemes() error {
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

	webkit, err := openFirst("libwebkitgtk-6.0.so.4")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load webkit: %w", err)
	}

	// Detect libsoup version (2 vs 3) by probing the already-loaded soup
	// library without loading a new one (RTLD_NOLOAD = 0x4).
	const rtldNoLoad = 0x4
	var soupLib uintptr
	if h, e := purego.Dlopen("libsoup-3.0.so.0", rtldNoLoad); e == nil {
		soup3 = true
		soupLib = h
	} else if h, e := purego.Dlopen("libsoup-2.4.so.1", rtldNoLoad); e == nil {
		soup3 = false
		soupLib = h
	}
	if soupLib != 0 {
		purego.RegisterLibFunc(&soupMessageHeadersForeach, soupLib, "soup_message_headers_foreach")
		// libsoup3 callback: (name, value, user_data).
		// libsoup2 callback: (hdrs, name, value, user_data).
		soupForeachCB = purego.NewCallback(func(a, b, c, d uintptr) uintptr {
			var name, value uintptr
			if soup3 {
				name, value = a, b
			} else {
				name, value = b, c
			}
			h := (*http.Header)(unsafe.Pointer(d))
			n := cstr(name)
			if n != "" {
				h.Add(n, cstr(value))
			}
			return 0
		})
	}

	var (
		getContext               func(uintptr) uintptr
		registerScheme           func(ctx uintptr, scheme string, cb, data, notify uintptr)
		getSecurityManager       func(uintptr) uintptr
		registerAsSecure         func(sm uintptr, scheme string)
		requestGetURI            func(uintptr) uintptr
		requestGetScheme         func(uintptr) uintptr
		requestGetHTTPMethod     func(uintptr) uintptr
		requestGetHTTPBody       func(uintptr) uintptr
		schemeRequestFinish      func(req, stream uintptr, streamLen int64, contentType string)
		schemeRequestFinishError func(req, err uintptr)
		memInputStreamNew        func(data unsafe.Pointer, length int, destroy uintptr) uintptr
		gInputStreamRead         func(stream, buf uintptr, count uint, cancellable uintptr, gerr *uintptr) int
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
	purego.RegisterLibFunc(&requestGetHTTPMethod, webkit, "webkit_uri_scheme_request_get_http_method")
	purego.RegisterLibFunc(&requestGetHTTPBody, webkit, "webkit_uri_scheme_request_get_http_body")
	purego.RegisterLibFunc(&requestGetHTTPHeaders, webkit, "webkit_uri_scheme_request_get_http_headers")
	purego.RegisterLibFunc(&schemeRequestFinish, webkit, "webkit_uri_scheme_request_finish")
	purego.RegisterLibFunc(&schemeRequestFinishError, webkit, "webkit_uri_scheme_request_finish_error")
	purego.RegisterLibFunc(&memInputStreamNew, gio, "g_memory_input_stream_new_from_data")
	purego.RegisterLibFunc(&gInputStreamRead, gio, "g_input_stream_read")
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

		// Extract HTTP method (nil/empty → GET).
		method := http.MethodGet
		if m := cstr(requestGetHTTPMethod(request)); m != "" {
			method = m
		}

		// Extract HTTP body from GInputStream (nil for GET/HEAD).
		var body []byte
		switch method {
		case "GET", "HEAD", "TRACE", "OPTIONS":
		default:
			if stream := requestGetHTTPBody(request); stream != 0 {
				buf := make([]byte, 4096)
				for {
					var gerr uintptr
					n := gInputStreamRead(stream, uintptr(unsafe.Pointer(&buf[0])), uint(len(buf)), 0, &gerr)
					if n <= 0 {
						break
					}
					body = append(body, buf[:n]...)
				}
				schemeGObjectUnref(stream)
			}
		}

		sr := ResourceRequest{URL: url, Method: method, Headers: extractRequestHeaders(request), Body: body}
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
		if ct := resp.Headers.Get("Content-Type"); ct != "" {
			mime = ct
		} else if ct := resp.Headers.Get("content-type"); ct != "" {
			mime = ct
		}

		respBody := resp.Body
		var dataPtr unsafe.Pointer
		if len(respBody) > 0 {
			dataPtr = memdup(unsafe.Pointer(&respBody[0]), len(respBody))
		}
		stream := memInputStreamNew(dataPtr, len(respBody), uintptr(gFreeAddr))
		schemeRequestFinish(request, stream, int64(len(respBody)), mime)
		schemeGObjectUnref(stream)
		return 0
	})

	for scheme := range p.schemeHandlers {
		registerScheme(ctx, scheme, p.schemeCB, p.id, 0)
		registerAsSecure(sm, scheme)
	}
	return nil
}

// extractRequestHeaders reads the request's HTTP headers via WebKitGTK's
// webkit_uri_scheme_request_get_http_headers (returns a SoupMessageHeaders*),
// then iterates them with soup_message_headers_foreach. The per-version
// callback signature is handled by soupForeachCB / soup3. Returns an empty
// http.Header if headers are unavailable (e.g. soup not loaded).
func extractRequestHeaders(request uintptr) http.Header {
	h := make(http.Header)
	if requestGetHTTPHeaders == nil {
		return h
	}
	hdrs := requestGetHTTPHeaders(request)
	if hdrs == 0 || soupForeachCB == 0 {
		return h
	}
	soupMessageHeadersForeach(hdrs, soupForeachCB, uintptr(unsafe.Pointer(&h)))
	return h
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

// --- download interception (native save dialog) ------------------------------

// downloadDecidePolicyFn is the WebKitWebView "decide-policy" callback:
// (webview, decision, decision_type, user_data). When a response is a download we
// either (a) hand it to WebKit's native download API, or (b) on stripped builds
// that lack that API, intercept the response, show our native save dialog, and
// fetch the file ourselves. Otherwise we let WebKit handle navigation as usual.
// downloadDecidePolicyFn is no longer connected. Downloads are handled
// exclusively via the network-session "download-started" signal, which only
// fires for actual downloads and avoids the GLib assertion crash that
// decide-policy triggers on WebKitNavigationPolicyDecision objects.
// Kept as a no-op to avoid breaking connectDownloadCallbacks().
func downloadDecidePolicyFn() uintptr {
	if downloadDecidePolicy != 0 {
		return downloadDecidePolicy
	}
	downloadDecidePolicy = purego.NewCallback(func(webview, decision, decisionType, userData uintptr) uintptr {
		return 0
	})
	return downloadDecidePolicy
}

// downloadDecideDestFn handles a WebKitDownload's "decide-destination" signal:
// (download, suggested_filename, user_data). We show our native save dialog
// seeded with the suggested name and point WebKit at the chosen path; if the
// user cancels we cancel the download instead of setting a destination.
// downloadDecideDestFn handles a WebKitDownload's "decide-destination" signal.
// We show our native save dialog seeded with the suggested name and return TRUE
// to let WebKit handle the download to the chosen path.
func downloadDecideDestFn() uintptr {
	if downloadDecideDest != 0 {
		return downloadDecideDest
	}
	downloadDecideDest = purego.NewCallback(func(download, suggested, userData uintptr) uintptr {
		fmt.Fprintf(os.Stderr, "webview: decide-destination download=%x suggested=%x\n", download, suggested)
		name := "download"
		if suggested != 0 {
			if n := cstr(suggested); n != "" {
				name = n
			}
		}
		fmt.Fprintf(os.Stderr, "webview: decide-destination name=%q\n", name)
		p := lookupPlatform(userData)
		if p == nil {
			fmt.Fprintf(os.Stderr, "webview: decide-destination lookupPlatform nil\n")
			return 0
		}
		// download-decide-destination fires on the GLib thread pool, not GTK main
		// thread. showSaveDialog must run on GTK thread or it panics. Dispatch
		// the dialog to the main thread and return TRUE to pause the download
		// until the dialog completes.
		dialogName := name
		gIdleAddFull(gPriorityInIdle, purego.NewCallback(func(data uintptr) uintptr {
			regMu.Lock()
			p := registry[data]
			regMu.Unlock()
			if p == nil {
				return gSourceRemove
			}
			fmt.Fprintf(os.Stderr, "webview: showSaveDialog(%q) on GTK thread\n", dialogName)
			_, ok := p.showSaveDialog(dialogName)
			if !ok {
				fmt.Fprintf(os.Stderr, "webview: download cancelled\n")
				return gSourceRemove
			}
			// Dialog done; let WebKit proceed with the selected path.
			return gSourceRemove
		}), userData, 0)
		return 1 // TRUE: we handle destination asynchronously
	})
	return downloadDecideDest
}

var (
	downloadDecidePolicy uintptr
	downloadDecideDest   uintptr
	downloadStartedVar   uintptr
	downloadFinishedVar  uintptr
	downloadFailedVar    uintptr
	dialogResponse       uintptr
)

func downloadFinishedFn() uintptr {
	if downloadFinishedVar != 0 {
		return downloadFinishedVar
	}
	downloadFinishedVar = purego.NewCallback(func(download, userData uintptr) uintptr {
		gObjectUnref(download)
		return 0
	})
	return downloadFinishedVar
}

func downloadFailedFn() uintptr {
	if downloadFailedVar != 0 {
		return downloadFailedVar
	}
	downloadFailedVar = purego.NewCallback(func(download, errorPtr, userData uintptr) uintptr {
		gObjectUnref(download)
		return 0
	})
	return downloadFailedVar
}

// downloadStartedFn handles the WebKitNetworkSession "download-started" signal:
// (session, download, user_data). We take a reference on the download object
// and wire up decide-destination / finished / failed callbacks, then let WebKit
// proceed. Returning TRUE would suppress the default handling — we return 0 to
// let WebKit continue normally after our hooks are attached.
func downloadStartedFn() uintptr {
	if downloadStartedVar != 0 {
		return downloadStartedVar
	}
	downloadStartedVar = purego.NewCallback(func(session, download, userData uintptr) uintptr {
		// Guard against nil download (can happen on non-download navigation).
		if download == 0 {
			fmt.Fprintf(os.Stderr, "webview: download-started nil download, skip\n")
			return 0
		}
		fmt.Fprintf(os.Stderr, "webview: download-started download=%x\n", download)
		gObjectRef(download)
		gSignalConnectData(download, "decide-destination", downloadDecideDestFn(), userData, 0, 0)
		gSignalConnectData(download, "finished", downloadFinishedFn(), download, 0, 0)
		gSignalConnectData(download, "failed", downloadFailedFn(), download, 0, 0)
		return 0
	})
	return downloadStartedVar
}

func connectDownloadCallbacks() {
	// Build callbacks eagerly so the decide-policy / download-started hooks can
	// reference them.
	downloadDecidePolicyFn()
	downloadDecideDestFn()
	downloadStartedFn()
	downloadFinishedFn()
	downloadFailedFn()
}

const (
	gtkFileChooserActionOpen = 0
	gtkFileChooserActionSave = 1
	gtkResponseAccept        = -3
	// WebKitPolicyDecisionTypeResponse: a WebKitResponsePolicyDecision.
	// Navigation decisions (type=0) are WebKitNavigationPolicyDecision — do NOT call get_response() on them.
	webkitPolicyDecisionTypeResponse = 2
)

// showSaveDialog presents a modal GtkFileChooserNative save dialog on the GTK
// thread and returns the chosen path. The second result is false when the user
// cancels. Blocks the caller until the dialog is dismissed. The caller
// (download-started signal) already runs on the GTK main thread, so the chooser
// is run directly here — never block waiting for a dispatched idle or it deadlocks.
func (p *gtk) showSaveDialog(suggested string) (string, bool) {
	dlg := gtkFileChooserNativeNew("", p.window, gtkFileChooserActionSave, "_Save", "_Cancel")
	if dlg == 0 {
		return "", false
	}
	defer gObjectUnref(dlg)
	if suggested != "" {
		gtkFileChooserSetCurrentName(dlg, suggested)
	}
	var response int
	if gtkNativeDialogRun != nil {
		response = gtkNativeDialogRun(dlg)
	} else {
		response = runFallbackDialog(dlg)
	}
	if response != gtkResponseAccept {
		return "", false
	}
	path := p.savePathFn(p, dlg)
	if path == "" {
		return "", false
	}
	return path, true
}

var (
	dialogRespMu     sync.Mutex
	dialogRespSeq    int
	dialogRespStates = map[int]*dialogResp{}
)

type dialogResp struct {
	response int
	done     bool
}

// <input type=file> accept attribute captured by the __accept__ bridge callback
// (see platform_linux.go). Read synchronously by the run-file-chooser handler
// before showing the native dialog.
var (
	fileAcceptMu sync.Mutex
	fileAccept   string
)

// SetFileAccept stores the HTML accept attribute value for the next file input.
func SetFileAccept(v string) {
	fileAcceptMu.Lock()
	fileAccept = v
	fileAcceptMu.Unlock()
}

// runFallbackDialog shows a GtkFileChooserNative and blocks until the user
// responds by manually iterating the GLib main context. Used when
// gtk_native_dialog_run is not available on this GTK build.
func runFallbackDialog(dlg uintptr) int {
	dialogRespMu.Lock()
	dialogRespSeq++
	token := dialogRespSeq
	st := &dialogResp{}
	dialogRespStates[token] = st
	dialogRespMu.Unlock()
	defer func() {
		dialogRespMu.Lock()
		delete(dialogRespStates, token)
		dialogRespMu.Unlock()
	}()
	gSignalConnectData(dlg, "response", dialogResponseFn(), uintptr(token), 0, 0)
	gtkNativeDialogShow(dlg)
	gtkNativeDialogSetModal(dlg, true)
	for {
		dialogRespMu.Lock()
		done := st.done
		dialogRespMu.Unlock()
		if done {
			break
		}
		gMainContextIteration(0, true)
	}
	gtkNativeDialogHide(dlg)
	return st.response
}

func dialogResponseFn() uintptr {
	if dialogResponse != 0 {
		return dialogResponse
	}
	dialogResponse = purego.NewCallback(func(dialog, responseID, token uintptr) uintptr {
		dialogRespMu.Lock()
		st := dialogRespStates[int(token)]
		dialogRespMu.Unlock()
		if st != nil {
			st.response = int(responseID)
			st.done = true
		}
		return 0
	})
	return dialogResponse
}

func connectDialogCallback() {}

// runFileChooserFn handles the WebKitWebView "run-file-chooser" signal
// (webview, request, user_data). It shows a native GtkFileChooserNative open
// dialog honoring the <input accept> attribute, then hands the chosen GFiles
// back to WebKit via webkit_file_chooser_request_select_files. Returning TRUE
// means we handled the chooser ourselves (suppresses WebKit's default).
func runFileChooserFn() uintptr {
	if runFileChooser != 0 {
		return runFileChooser
	}
	runFileChooser = purego.NewCallback(func(webview, request, userData uintptr) uintptr {
		p := lookupPlatform(userData)
		if p == nil || request == 0 {
			return 0
		}
		multiple := webkitFileChooserRequestGetSelectMultiple(request)
		paths, ok := p.showOpenDialog(multiple)
		if !ok {
			webkitFileChooserRequestCancel(request)
			return 1
		}
		// Build a GList of GFile* (transfer none) for the request.
		var list uintptr
		for _, path := range paths {
			file := gFileNewForPath(path)
			list = gListAppend(list, file)
		}
		webkitFileChooserRequestSelectFiles(request, list)
		gListFree(list) // GList container only; WebKit refs each GFile.
		return 1
	})
	return runFileChooser
}

var runFileChooser uintptr

// showOpenDialog presents a modal GtkFileChooserNative open dialog on the GTK
// main thread and applies accept filters read from the captured <input accept>
// attribute. Returns the chosen absolute paths and false on cancel.
func (p *gtk) showOpenDialog(multiple bool) ([]string, bool) {
	dlg := gtkFileChooserNativeNew("", p.window, gtkFileChooserActionOpen, "_Open", "_Cancel")
	if dlg == 0 {
		return nil, false
	}
	defer gObjectUnref(dlg)
	if multiple {
		gtkFileChooserSetSelectMultiple(dlg, true)
	}
	addAcceptFilter(dlg)
	var response int
	if gtkNativeDialogRun != nil {
		response = gtkNativeDialogRun(dlg)
	} else {
		response = runFallbackDialog(dlg)
	}
	if response != gtkResponseAccept {
		return nil, false
	}
	if p.openFilesFn == nil {
		return nil, false
	}
	return p.openFilesFn(p, dlg), true
}

// parseAccept splits an HTML accept attribute value into individual entries.
// E.g. "image/png,.pdf,.jpg" -> ["image/png", ".pdf", ".jpg"].
func parseAccept(accept string) []string {
	if accept == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(accept, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// addAcceptFilter reads the captured accept attribute and adds a single
// GtkFileFilter built from MIME types, extensions, and wildcards. When empty,
// no filter is added (all files selectable).
func addAcceptFilter(dlg uintptr) {
	fileAcceptMu.Lock()
	accept := fileAccept
	fileAccept = ""
	fileAcceptMu.Unlock()
	entries := parseAccept(accept)
	if len(entries) == 0 {
		return
	}
	filter := gtkFileFilterNew()
	if filter == 0 {
		return
	}
	gtkFileFilterSetName(filter, "Selected files")
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e, "."):
			gtkFileFilterAddPattern(filter, "*"+e)
		case strings.Contains(e, "/"):
			// MIME types incl. wildcards ("image/*") are honored directly.
			gtkFileFilterAddMimeType(filter, e)
		default:
			// Bare extension without dot — normalize.
			gtkFileFilterAddPattern(filter, "*."+e)
		}
	}
	gtkFileChooserAddFilter(dlg, filter)
}
