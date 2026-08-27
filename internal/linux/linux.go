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
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode"
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
	gtkWindowToplevel = 0

	gPriorityHighIdle = 100
	gSourceRemove     = 0

	injectTopFrame        = 1 // WEBKIT_USER_CONTENT_INJECT_TOP_FRAME
	injectAtDocumentStart = 0 // WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START

	gdkHintMaxSize   = 1 << 2 // GDK_HINT_MAX_SIZE
	gSignalMatchData = 1 << 4 // G_SIGNAL_MATCH_DATA

	defaultWidth  = 640
	defaultHeight = 480

	gdkControlMask = 1 << 2 // GDK_CONTROL_MASK

	gtkAccelVisible = 1 << 0 // GTK_ACCEL_VISIBLE

	// GTK box orientation (gtk_box_new).
	gtkOrientationVertical = 1 // GTK_ORIENTATION_VERTICAL
	gSignalActivate = "activate"
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
	gObjectRef                       func(obj uintptr) uintptr
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
	gtkWidgetQueueDraw        func(widget uintptr)
	gtkWidgetGetToplevel      func(widget uintptr) uintptr
	gtkWidgetGetWindow        func(widget uintptr) uintptr
	gdkWindowProcessUpdates   func(window uintptr, invalidateChildren bool)
	gtkMenuBarNew             func() uintptr
	gtkMenuNew                func() uintptr
	gtkMenuItemNewWithLabel   func(label string) uintptr
	gtkMenuItemSetSubmenu     func(menuItem, submenu uintptr)
	gtkMenuShellAppend        func(shell, child uintptr)
	gtkSeparatorMenuItemNew   func() uintptr
	gtkAccelGroupNew          func() uintptr
	gtkWindowAddAccelGroup    func(window, accelGroup uintptr)
	gtkWidgetAddAccelerator   func(widget uintptr, accelSignal string, accelGroup uintptr, accelKey uint, mods, flags int)
	gtkVboxNew                func(homogeneous bool, spacing int) uintptr
	gtkBoxPackStart           func(box, child uintptr, expand, fill int, padding uint)
	gtkWidgetGrabFocus        func(widget uintptr)
	gtkWindowPresent          func(window uintptr)
	gtkWindowClose            func(window uintptr)
	gtkWindowSetPosition      func(window uintptr, position int)

	// GTK file chooser (native save dialog for downloads) + GTK4 box packing.
	gtkFileChooserNativeNew  func(title string, parent uintptr, action int, accept, cancel string) uintptr
	gtkNativeDialogShow      func(dialog uintptr)
	gtkNativeDialogHide      func(dialog uintptr)
	gtkNativeDialogSetModal  func(dialog uintptr, modal bool)
	gtkFileChooserSetCurrentName func(chooser uintptr, name string)
	gtkFileChooserGetFilename func(chooser uintptr) uintptr // char* (GTK3)
	gtkFileChooserGetFile    func(chooser uintptr) uintptr // GFile* (GTK4)
	gFileGetPath             func(file uintptr) uintptr     // char* (GTK4)
	gtkBoxNew                func(orientation int, spacing int) uintptr
	gtkBoxAppend             func(box, child uintptr)
	gtkWidgetInsertActionGroup func(widget uintptr, groupName string, group uintptr)

	// GMenu / GAction for GTK4 menubar.
	gMenuNew                func() uintptr
	gMenuAppend             func(menu uintptr, label, detailedAction string)
	gMenuAppendSubmenu      func(menu uintptr, label string, submenu uintptr)
	gMenuAppendSection      func(menu uintptr, label string, section uintptr)
	gSimpleActionNew        func(name string, parameterType uintptr) uintptr
	gSimpleActionGroupNew   func() uintptr
	gSimpleActionGroupInsert func(group, action uintptr)
	gtkPopoverMenuBarNewFromModel func(model uintptr) uintptr

	// WebKitGTK URI request header accessor + libsoup foreach (set in
	// registerSchemes; libsoup 2 vs 3 differ in foreach callback signature).
	requestGetHTTPHeaders     func(uintptr) uintptr
	soupMessageHeadersForeach func(hdrs, cb, userData uintptr)

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

	// Download via the modern, always-exported API: a web-context
	// "download-started" callback plus each download's "decide-destination"
	// signal (which already hands us the suggested filename). The older
	// response-policy-decision helpers are not exported on every WebKit2GTK
	// build, so we avoid them.
	webkitWebViewGetContext                    func(webview uintptr) uintptr
	webkitDownloadSetDestination               func(download uintptr, destination string)
	webkitDownloadCancel                       func(download uintptr)
	hasDownloadSupport                         bool
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
		gio, err := openFirst("libgio-2.0.so.0")
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

		if gtk4 {
			purego.RegisterLibFunc(&gtkInitCheck0, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew0, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkWindowSetChild, gtk, "gtk_window_set_child")
			purego.RegisterLibFunc(&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible")
			purego.RegisterLibFunc(&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size")
			// GTK4-only symbols (absent in GTK3's libgtk-3).
			purego.RegisterLibFunc(&gtkBoxAppend, gtk, "gtk_box_append")
			purego.RegisterLibFunc(&gtkWidgetInsertActionGroup, gtk, "gtk_widget_insert_action_group")
			purego.RegisterLibFunc(&gtkPopoverMenuBarNewFromModel, gtk, "gtk_popover_menu_bar_new_from_model")
		} else {
			purego.RegisterLibFunc(&gtkInitCheck, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkContainerAdd, gtk, "gtk_container_add")
			purego.RegisterLibFunc(&gtkContainerRemove, gtk, "gtk_container_remove")
			purego.RegisterLibFunc(&gtkWidgetShow, gtk, "gtk_widget_show")
			purego.RegisterLibFunc(&gtkWidgetQueueDraw, gtk, "gtk_widget_queue_draw")
			purego.RegisterLibFunc(&gtkWidgetGetToplevel, gtk, "gtk_widget_get_toplevel")
			purego.RegisterLibFunc(&gtkWidgetGetWindow, gtk, "gtk_widget_get_window")
			purego.RegisterLibFunc(&gdkWindowProcessUpdates, gtk, "gdk_window_process_updates")
			purego.RegisterLibFunc(&gtkWidgetShowAll, gtk, "gtk_widget_show_all")
			purego.RegisterLibFunc(&gtkWindowResize, gtk, "gtk_window_resize")
			purego.RegisterLibFunc(&gtkWindowSetGeometryHints, gtk, "gtk_window_set_geometry_hints")
			purego.RegisterLibFunc(&gtkMenuBarNew, gtk, "gtk_menu_bar_new")
			purego.RegisterLibFunc(&gtkMenuNew, gtk, "gtk_menu_new")
			purego.RegisterLibFunc(&gtkMenuItemNewWithLabel, gtk, "gtk_menu_item_new_with_label")
			purego.RegisterLibFunc(&gtkMenuItemSetSubmenu, gtk, "gtk_menu_item_set_submenu")
			purego.RegisterLibFunc(&gtkMenuShellAppend, gtk, "gtk_menu_shell_append")
			purego.RegisterLibFunc(&gtkSeparatorMenuItemNew, gtk, "gtk_separator_menu_item_new")
			purego.RegisterLibFunc(&gtkAccelGroupNew, gtk, "gtk_accel_group_new")
			purego.RegisterLibFunc(&gtkWindowAddAccelGroup, gtk, "gtk_window_add_accel_group")
			purego.RegisterLibFunc(&gtkWidgetAddAccelerator, gtk, "gtk_widget_add_accelerator")
			purego.RegisterLibFunc(&gtkVboxNew, gtk, "gtk_vbox_new")
			purego.RegisterLibFunc(&gtkBoxPackStart, gtk, "gtk_box_pack_start")
		}
		purego.RegisterLibFunc(&gtkWindowSetTitle, gtk, "gtk_window_set_title")
		purego.RegisterLibFunc(&gtkWindowSetResizable, gtk, "gtk_window_set_resizable")
		purego.RegisterLibFunc(&gtkWidgetSetSizeRequest, gtk, "gtk_widget_set_size_request")
		purego.RegisterLibFunc(&gtkWidgetGrabFocus, gtk, "gtk_widget_grab_focus")
		purego.RegisterLibFunc(&gtkWindowPresent, gtk, "gtk_window_present")
		purego.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")
		purego.RegisterLibFunc(&gtkWindowSetPosition, gtk, "gtk_window_set_position")
		// Shared across GTK3 (>=3.20) and GTK4.
		purego.RegisterLibFunc(&gtkFileChooserNativeNew, gtk, "gtk_file_chooser_native_new")
		purego.RegisterLibFunc(&gtkNativeDialogShow, gtk, "gtk_native_dialog_show")
		purego.RegisterLibFunc(&gtkNativeDialogHide, gtk, "gtk_native_dialog_hide")
		purego.RegisterLibFunc(&gtkNativeDialogSetModal, gtk, "gtk_native_dialog_set_modal")
		purego.RegisterLibFunc(&gtkFileChooserSetCurrentName, gtk, "gtk_file_chooser_set_current_name")
		purego.RegisterLibFunc(&gtkFileChooserGetFilename, gtk, "gtk_file_chooser_get_filename")
		purego.RegisterLibFunc(&gtkFileChooserGetFile, gtk, "gtk_file_chooser_get_file")
		// g_file_get_path lives in libgio (GTK4 save-path result).
		purego.RegisterLibFunc(&gFileGetPath, gio, "g_file_get_path")

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

		// Download support uses only always-exported WebKit2 symbols.
		// Register WebKit download symbols. We connect the "download-started"
		// GObject signal directly via g_signal_connect_data (portable across
		// all WebKit2GTK versions) instead of relying on the optional
		// webkit_web_context_set_download_started_callback C wrapper, which is
		// absent in some builds and would silently disable downloads.
		purego.RegisterLibFunc(&webkitWebViewGetContext, webkit, "webkit_web_view_get_context")
		purego.RegisterLibFunc(&webkitDownloadSetDestination, webkit, "webkit_download_set_destination")
		purego.RegisterLibFunc(&webkitDownloadCancel, webkit, "webkit_download_cancel")
		hasDownloadSupport = true

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
	regMu     sync.Mutex
	registry  = map[uintptr]*Platform{}
	engineSeq uintptr

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
	menuBar       uintptr

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
func (p *Platform) SetMenus(menus []Menu) {
	p.pendingMenus = menus
	p.hasCustomMenus = len(menus) > 0
}

// MainThread runs f on the GTK main thread, blocking until it completes.
func (p *Platform) MainThread(f func()) {
	done := make(chan struct{})
	dispatchMain(func() {
		f()
		close(done)
	})
	<-done
}

// Close destroys the window and signals the main loop to stop.
func (p *Platform) Close() error {
	if p.window != 0 {
		dispatchMain(func() { gtkWindowClose(p.window) })
	} else {
		p.stopRunLoop = true
	}
	return nil
}

// menuRedrawFn repaints the toplevel window when a popup submenu is dismissed,
// clearing the ghost rectangle left on GTK3 with no window manager compositor.
var menuRedrawFn uintptr

// winRedrawIdle repaints a window on the next idle, after any popup teardown
// has fully settled. Used to clear the GTK3 ghost-popup rectangle. On systems
// without a compositing window manager the popup's GdkWindow unmaps but the
// exposed region behind it isn't repainted, so we explicitly process updates
// on the parent toplevel's GdkWindow.
var winRedrawIdle uintptr

func winRedrawIdleBuild() uintptr {
	if winRedrawIdle != 0 {
		return winRedrawIdle
	}
	winRedrawIdle = purego.NewCallback(func(widget uintptr) uintptr {
		if widget != 0 {
			gtkWidgetQueueDraw(widget)
			win := gtkWidgetGetWindow(widget)
			if win != 0 {
				gdkWindowProcessUpdates(win, true)
			}
		}
		return gSourceRemove
	})
	return winRedrawIdle
}

// queueWinRedraw schedules a toplevel repaint on the next idle.
func queueWinRedraw(widget uintptr) {
	gIdleAddFull(gPriorityHighIdle, winRedrawIdleBuild(), widget, 0)
}

func menuRedrawFnBuild() uintptr {
	if menuRedrawFn != 0 {
		return menuRedrawFn
	}
	menuRedrawFn = purego.NewCallback(func(menu, userData uintptr) uintptr {
		toplevel := gtkWidgetGetToplevel(userData)
		if toplevel != 0 {
			queueWinRedraw(toplevel)
		}
		return 0
	})
	return menuRedrawFn
}

// wireMenuRedraw connects a submenu's "hide" signal so the toplevel window is
// repainted once the popup is actually removed from the screen (GHOST-popup
// fix on GTK3 no-compositor). "hide" fires after the popup unmaps, which is the
// correct time to clear the leftover rectangle.
func wireMenuRedraw(submenu, toplevel uintptr) {
	gSignalConnectData(submenu, "hide", menuRedrawFnBuild(), toplevel, 0, 0)
}

// GTK4 menubar: GMenu model + GSimpleAction group. Each item gets its own
// activate callback closure (keyed by action pointer) so we don't depend on
// g_simple_action_get_name, which some libgio builds don't export.
var (
	gtk4MenuActionMu sync.Mutex
	gtk4MenuActions  = map[uintptr]func(){}
)

func (p *Platform) buildMenubarGTK4(menus []Menu) {
	gtk4MenuActionMu.Lock()
	gtk4MenuActions = map[uintptr]func(){}
	gtk4MenuActionMu.Unlock()

	group := gSimpleActionGroupNew()
	root := gMenuNew()
	idx := 0
	for _, m := range menus {
		sub := gMenuNew()
		for _, mi := range m.Items {
			if mi.Separator {
				continue
			}
			name := "a" + strconv.Itoa(idx)
			idx++
			gMenuAppend(sub, mi.Label, "win."+name)
			if mi.Action != nil {
				cb := mi.Action
				act := gSimpleActionNew(name, 0)
				gtk4MenuActionMu.Lock()
				gtk4MenuActions[act] = cb
				gtk4MenuActionMu.Unlock()
				gSignalConnectData(act, gSignalActivate, gtk4MenuActivateFn(), 0, 0, 0)
				gSimpleActionGroupInsert(group, act)
			}
		}
		gMenuAppendSubmenu(root, m.Label, sub)
	}
	gtkWidgetInsertActionGroup(p.window, "win", group)
	p.menuBar = gtkPopoverMenuBarNewFromModel(root)
}

// gtk4MenuActivateFn returns the single activate callback (built once). It looks
// up the clicked action by pointer in gtk4MenuActions.
func gtk4MenuActivateFn() uintptr {
	if gtk4MenuActivate != 0 {
		return gtk4MenuActivate
	}
	gtk4MenuActivate = purego.NewCallback(func(action, param, userData uintptr) uintptr {
		gtk4MenuActionMu.Lock()
		cb := gtk4MenuActions[action]
		gtk4MenuActionMu.Unlock()
		if cb != nil {
			cb()
		}
		return 0
	})
	return gtk4MenuActivate
}

var gtk4MenuActivate uintptr

// menuCustomCBMap stores custom menu item callbacks (by item pointer).
var menuCustomCBMu sync.Mutex
var menuCustomCBMap = map[uintptr]func(){}

// soup3 is true when the loaded WebKitGTK links libsoup 3 (webkit2gtk-4.1 /
// webkitgtk-6.0); false for libsoup 2 (webkit2gtk-4.0). The two differ in the
// soup_message_headers_foreach callback signature.
var soup3 bool

// soupForeachCB is the pre-built purego callback for soup_message_headers_foreach,
// chosen by soup3. Set once in registerSchemes.
var soupForeachCB uintptr

// applyMenus builds and installs a menu bar from the given Menu slice.
// GTK4 uses a GMenu/GAction popover bar; GTK3 uses a classic GtkMenuBar.
func (p *Platform) applyMenus(menus []Menu) {
	if gtk4 {
		p.buildMenubarGTK4(menus)
		return
	}
	// Clear old custom callbacks.
	menuCustomCBMu.Lock()
	menuCustomCBMap = map[uintptr]func(){}
	menuCustomCBMu.Unlock()

	menuBar := gtkMenuBarNew()
	accelGroup := gtkAccelGroupNew()
	gtkWindowAddAccelGroup(p.window, accelGroup)

	// Register a callback that dispatches to menuCustomCBMap.
	customMenuActionFn = purego.NewCallback(func(item, userData uintptr) uintptr {
		menuCustomCBMu.Lock()
		cb := menuCustomCBMap[item]
		menuCustomCBMu.Unlock()
		if cb != nil {
			cb()
		}
		return 0
	})

	for _, m := range menus {
		topItem := gtkMenuItemNewWithLabel(m.Label)
		submenu := gtkMenuNew()
		gtkMenuItemSetSubmenu(topItem, submenu)
		gtkMenuShellAppend(menuBar, topItem)

		for _, mi := range m.Items {
			if mi.Separator {
				gtkMenuShellAppend(submenu, gtkSeparatorMenuItemNew())
				continue
			}
			item := gtkMenuItemNewWithLabel(mi.Label)
			if mi.Action != nil {
				gSignalConnectData(item, gSignalActivate, customMenuActionFn, 0, 0, 0)
				menuCustomCBMu.Lock()
				menuCustomCBMap[item] = mi.Action
				menuCustomCBMu.Unlock()
			}
			if mi.Shortcut != "" {
				key, mods := parseGtkShortcut(mi.Shortcut)
				if key != 0 {
					gtkWidgetAddAccelerator(item, gSignalActivate, accelGroup, key, mods, gtkAccelVisible)
				}
			}
			gtkMenuShellAppend(submenu, item)
		}
		wireMenuRedraw(submenu, p.window)
	}

	p.menuBar = menuBar
}

var customMenuActionFn uintptr

// parseGtkShortcut parses "Ctrl+Z" into (gdk_keyval, GdkModifierType).
func parseGtkShortcut(s string) (uint, int) {
	var mods int
	var key uint
	for _, part := range strings.Split(s, "+") {
		switch strings.TrimSpace(part) {
		case "Ctrl", "Control":
			mods |= gdkControlMask
		case "Shift":
			mods |= 1 << 0 // GDK_SHIFT_MASK
		case "Alt":
			mods |= 1 << 3 // GDK_MOD1_MASK
		case "Super", "Cmd", "Meta":
			mods |= 1 << 6 // GDK_SUPER_MASK
		default:
			k := strings.TrimSpace(part)
			if len(k) == 1 {
				key = uint(unicode.ToLower(rune(k[0])))
			}
		}
	}
	return key, mods
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

	if hasDownloadSupport {
		ctx := webkitWebViewGetContext(p.webview)
		gSignalConnectData(ctx, "download-started", downloadStartedFn(), p.id, 0, 0)
	}

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

// Init injects JavaScript that runs at the start of every new page load.
func (p *Platform) Init(js string) error {
	p.pushUserScript(js)
	return nil
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
		box := gtkBoxNew(gtkOrientationVertical, 0)
		if p.menuBar != 0 {
			gtkBoxAppend(box, p.menuBar)
		}
		gtkBoxAppend(box, p.webview)
		gtkWindowSetChild(p.window, box)
		gtkWidgetSetVisible(p.webview, true)
		gtkWidgetSetVisible(p.window, true)
	} else {
		box := gtkVboxNew(false, 0)
		if p.menuBar != 0 {
			// Menubar is fixed height (no expand/fill).
			gtkBoxPackStart(box, p.menuBar, 0, 0, 0)
		}
		// Webview expands to fill the remaining window space.
		gtkBoxPackStart(box, p.webview, 1, 1, 0)
		gtkContainerAdd(p.window, box)
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

// downloadStartedFn is the WebKitWebContext "download-started" callback:
// (context, download, user_data). We attach the download's "decide-destination"
// signal so WebKit waits for us to pick a path through our own native save
// dialog instead of navigating or spawning WebKit's download UI.
func downloadStartedFn() uintptr {
	if downloadStarted != 0 {
		return downloadStarted
	}
	downloadStarted = purego.NewCallback(func(context, download, userData uintptr) uintptr {
		// WebKit frees the download object right after this callback returns,
		// so take a reference and hold it until finished/failed.
		gObjectRef(download)
		gSignalConnectData(download, "decide-destination", downloadDecideDestFn(), userData, 0, 0)
		gSignalConnectData(download, "finished", downloadFinishedFn(), download, 0, 0)
		gSignalConnectData(download, "failed", downloadFailedFn(), download, 0, 0)
		return 0
	})
	return downloadStarted
}

// downloadDecideDestFn handles a WebKitDownload's "decide-destination" signal:
// (download, suggested_filename, user_data). We show our native save dialog
// seeded with the suggested name and point WebKit at the chosen path; if the
// user cancels we cancel the download instead of setting a destination.
func downloadDecideDestFn() uintptr {
	if downloadDecideDest != 0 {
		return downloadDecideDest
	}
	downloadDecideDest = purego.NewCallback(func(download, suggested, userData uintptr) uintptr {
		name := "download"
		if suggested != 0 {
			if n := cstr(suggested); n != "" {
				name = n
			}
		}
		p := lookupPlatform(userData)
		if p == nil {
			webkitDownloadCancel(download)
			return 1 // TRUE: we handled the destination (cancel), stop default
		}
		path, ok := p.showSaveDialog(name)
		if !ok || path == "" {
			webkitDownloadCancel(download)
			return 1 // TRUE: we handled the destination (cancel), stop default
		}
		// WebKitGTK 2.40+ set_destination takes a raw absolute path (no file:// URI).
		webkitDownloadSetDestination(download, path)
		return 1 // TRUE: destination set, proceed with download
	})
	return downloadDecideDest
}

var (
	downloadStarted     uintptr
	downloadDecideDest  uintptr
	downloadFinishedVar uintptr
	downloadFailedVar   uintptr
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

func connectDownloadCallbacks() {
	// Build callbacks eagerly so the download-started hook can reference them.
	downloadStartedFn()
	downloadDecideDestFn()
	downloadFinishedFn()
	downloadFailedFn()
}

const (
	gtkFileChooserActionSave = 1
	gtkResponseAccept        = -3
)

// showSaveDialog presents a modal GtkFileChooserNative save dialog on the GTK
// thread and returns the chosen path. The second result is false when the user
// cancels. Blocks the caller until the dialog is dismissed.
// showSaveDialog presents a modal GtkFileChooserNative save dialog and returns
// the chosen path. The second result is false when the user cancels. The caller
// (decide-policy signal) already runs on the GTK main thread, so the chooser is
// run directly here — never block waiting for a dispatched idle or it deadlocks.
func (p *Platform) showSaveDialog(suggested string) (string, bool) {
	path := runSaveChooser(p.window, suggested)
	if path == "" {
		return "", false
	}
	return path, true
}

// dialogResponseFn reports a single modal dialog's response, keyed by an integer
// token passed as the signal user_data (only integers cross into C).
var dialogResponseFn uintptr

type dialogResp struct {
	response int
	done     bool
}

var (
	dialogRespMu     sync.Mutex
	dialogRespStates = map[uintptr]*dialogResp{}
	dialogRespSeq    uintptr
)

func connectDialogCallback() {
	if dialogResponseFn != 0 {
		return
	}
	dialogResponseFn = purego.NewCallback(func(dialog, responseID, token uintptr) uintptr {
		dialogRespMu.Lock()
		st := dialogRespStates[token]
		if st != nil {
			st.response = int(int32(uint32(responseID)))
			st.done = true
		}
		dialogRespMu.Unlock()
		return 0
	})
}

func runSaveChooser(parent uintptr, suggested string) string {
	dlg := gtkFileChooserNativeNew("", parent, gtkFileChooserActionSave, "_Save", "_Cancel")
	if dlg == 0 {
		return ""
	}
	defer gObjectUnref(dlg)
	if suggested != "" {
		gtkFileChooserSetCurrentName(dlg, suggested)
	}
	if runNativeDialog(dlg) != gtkResponseAccept {
		return ""
	}
	if gtk4 {
		file := gtkFileChooserGetFile(dlg) // transfer full
		if file == 0 {
			return ""
		}
		defer gObjectUnref(file)
		if cs := gFileGetPath(file); cs != 0 {
			return cstr(cs)
		}
		return ""
	}
	cs := gtkFileChooserGetFilename(dlg) // char*, owned by caller
	if cs == 0 {
		return ""
	}
	defer gFree(cs)
	return cstr(cs)
}

// runNativeDialog shows a GtkNativeDialog modally and pumps the main loop until
// the user responds, returning the response id. Replaces the GTK4-removed
// gtk_native_dialog_run with its underlying mechanism (show + "response" +
// nested iteration), so it works on both GTK3 and GTK4.
func runNativeDialog(dlg uintptr) int {
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

	gSignalConnectData(dlg, "response", dialogResponseFn, token, 0, 0)
	gtkNativeDialogSetModal(dlg, true)
	gtkNativeDialogShow(dlg)
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
