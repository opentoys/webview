package linux

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// GTK4-specific implementation: GMenu/GAction popover menubar + box packing.
type gtk4 struct {
	*gtk
}

// newgtk4 wires up the GTK4 version of every function-field hook and registers
// the GTK4-only purego symbols. Returns nil on success.
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
	// File chooser / filter (input type=file + accept).
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

func pgtk4CreateWindow(p *gtk) error {
	if !gtkInitCheck4() {
		return errNoDisplay
	}
	p.window = gtkWindowNew4()
	return nil
}

// buildMenubar builds a GMenu/GAction popover menu bar (GtkMenuBar is gone in
// GTK4). Each non-separator item is wired to a GSimpleAction whose "activate"
// signal dispatches the Go callback stored in gtk4MenuActions.
func pgtk4BuildMenubar(p *gtk, menus []Menu) {
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

func pgtk4ShowWindow(p *gtk) {
	box := gtkBoxNew(gtkOrientationVertical, 0)
	if p.menuBar != 0 {
		gtkBoxAppend(box, p.menuBar)
		applyMenubarBorderFix()
	}
	// gtk_box_append defaults to no expansion, so the webview collapses to its
	// minimum size (white screen). Force it to fill the window.
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
	// SizeMin / SizeNone / SizeFixed all map to default size on GTK4.
	gtkWindowSetDefaultSize(p.window, width, height)
}

func pgtk4SavePath(p *gtk, dlg uintptr) string {
	file := gtkFileChooserGetFile(dlg) // transfer full
	if file == 0 {
		return ""
	}
	defer gObjectUnref(file)
	cs := gFileGetPath(file)
	if cs == 0 {
		return ""
	}
	return cstr(cs)
}

func pgtk4MessageValue(p *gtk, arg uintptr) string {
	cs := jscValueToString(arg)
	s := cstr(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

func pgtk4RegisterScriptHandler(p *gtk, manager uintptr, name string) {
	webkitRegisterHandler3(manager, name, 0)
}

// pgtk4OpenFiles reads the chosen files from a GTK4 GtkFileChooserNative. GTK4
// returns a GList* of GFile* (transfer full); we convert each to a path, free
// the char*, and unref the GFile.
func pgtk4OpenFiles(p *gtk, dlg uintptr) []string {
	files := gtkFileChooserGetFiles(dlg)
	if files == 0 {
		return nil
	}
	defer gListFree(files)
	n := int(gListLength(files))
	var out []string
	for i := uint(0); i < uint(n); i++ {
		file := gListNthData(files, i)
		if file == 0 {
			continue
		}
		cs := gFileGetPath(file)
		if cs != 0 {
			if s := cstr(cs); s != "" {
				out = append(out, s)
			}
			gFree(cs)
		}
		gObjectUnref(file)
	}
	return out
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

// applyMenubarBorderFix injects a global CSS provider that unconditionally
// removes the shadow/border around all popovers (including the menu bar
// submenu popovers). Forced on for all environments.
func applyMenubarBorderFix() {
	if gdkDisplayGetDefault == nil {
		return
	}
	display := gdkDisplayGetDefault()
	if display == 0 {
		return
	}
	css := []byte("popover {" +
		"  box-shadow: none;" +
		"  border: none;" +
	"}" +
		"popover contents {" +
		"  border-radius: 0;" +
		"  box-shadow: none;" +
		"}" +
		"popover arrow {" +
		"  background: transparent;" +
		"  border-color: transparent;" +
		"}\x00")
	provider := gtkCssProviderNew()
	var gerr uintptr
	gtkCssProviderLoadFromData(provider, uintptr(unsafe.Pointer(&css[0])), int64(len(css)-1), gerr)
	if gerr != 0 {
		gObjectUnref(provider)
		return
	}
	gtkStyleContextAddProviderForDisplay(display, provider, gtkStyleProviderPriorityApplication)
	gObjectUnref(provider)
	fmt.Fprintf(os.Stderr, "webview: applied popover shadow removal\n")
}

var (
	gtk4MenuActionMu sync.Mutex
	gtk4MenuActions  = map[uintptr]func(){}
	gtk4MenuActivate uintptr
)
