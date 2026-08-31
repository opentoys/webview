//go:build linux

package linux

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

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
	// File chooser filters.
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

// newgtk4symbols registers symbols used only by the GTK4 implementation.
func newgtk4symbols() {
	gtk := libGTK
	purego.RegisterLibFunc(&gtkInitCheck4, gtk, "gtk_init_check")
	purego.RegisterLibFunc(&gtkWindowNew4, gtk, "gtk_window_new")
	purego.RegisterLibFunc(&gtkWindowSetChild, gtk, "gtk_window_set_child")
	purego.RegisterLibFunc(&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible")
	purego.RegisterLibFunc(&gtkWidgetSetHExpand, gtk, "gtk_widget_set_hexpand")
	purego.RegisterLibFunc(&gtkWidgetSetVExpand, gtk, "gtk_widget_set_vexpand")
	purego.RegisterLibFunc(&gtkBoxNew, gtk, "gtk_box_new")
	purego.RegisterLibFunc(&gtkBoxAppend, gtk, "gtk_box_append")
	purego.RegisterLibFunc(&gtkWidgetInsertActionGroup, gtk, "gtk_widget_insert_action_group")
	purego.RegisterLibFunc(&gtkPopoverMenuBarNewFromModel, gtk, "gtk_popover_menu_bar_new_from_model")
	purego.RegisterLibFunc(&gtkCssProviderNew, gtk, "gtk_css_provider_new")
	purego.RegisterLibFunc(&gtkCssProviderLoadFromData, gtk, "gtk_css_provider_load_from_data")
	purego.RegisterLibFunc(&gtkStyleContextAddClass, gtk, "gtk_style_context_add_class")
	purego.RegisterLibFunc(&gtkWidgetGetStyleContext, gtk, "gtk_widget_get_style_context")
	if libGDK != 0 {
		purego.RegisterLibFunc(&gdkDisplayGetDefault, libGDK, "gdk_display_get_default")
		purego.RegisterLibFunc(&gdkDisplayIsComposited, libGDK, "gdk_display_is_composited")
	}
	purego.RegisterLibFunc(&gtkStyleContextAddProviderForDisplay, gtk, "gtk_style_context_add_provider_for_display")
	purego.RegisterLibFunc(&webkitRegisterHandler3, libWebKit, "webkit_user_content_manager_register_script_message_handler")

	// File chooser / filter (<input type=file> + accept).
	purego.RegisterLibFunc(&gtkFileChooserSetSelectMultiple, gtk, "gtk_file_chooser_set_select_multiple")
	purego.RegisterLibFunc(&gtkFileChooserAddFilter, gtk, "gtk_file_chooser_add_filter")
	purego.RegisterLibFunc(&gtkFileFilterNew, gtk, "gtk_file_filter_new")
	purego.RegisterLibFunc(&gtkFileFilterSetName, gtk, "gtk_file_filter_set_name")
	purego.RegisterLibFunc(&gtkFileFilterAddMimeType, gtk, "gtk_file_filter_add_mime_type")
	purego.RegisterLibFunc(&gtkFileFilterAddPattern, gtk, "gtk_file_filter_add_pattern")
	purego.RegisterLibFunc(&webkitFileChooserRequestGetSelectMultiple, libWebKit, "webkit_file_chooser_request_get_select_multiple")
	purego.RegisterLibFunc(&webkitFileChooserRequestSelectFiles, libWebKit, "webkit_file_chooser_request_select_files")
	purego.RegisterLibFunc(&webkitFileChooserRequestCancel, libWebKit, "webkit_file_chooser_request_cancel")
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
