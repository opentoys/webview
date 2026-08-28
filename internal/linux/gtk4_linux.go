package linux

import (
	"strconv"
	"sync"

	"github.com/ebitengine/purego"
)

// GTK4-specific implementation: GMenu/GAction popover menubar + box packing.

type gtk4Backend struct {
	*Platform
}

// registerSymbols binds the GTK4-only C functions (absent in libgtk-3).
func (b *gtk4Backend) registerSymbols() {
	gtk := libGTK
	purego.RegisterLibFunc(&gtkInitCheck0, gtk, "gtk_init_check")
	purego.RegisterLibFunc(&gtkWindowNew0, gtk, "gtk_window_new")
	purego.RegisterLibFunc(&gtkWindowSetChild, gtk, "gtk_window_set_child")
	purego.RegisterLibFunc(&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible")
	purego.RegisterLibFunc(&gtkWidgetSetHExpand, gtk, "gtk_widget_set_hexpand")
	purego.RegisterLibFunc(&gtkWidgetSetVExpand, gtk, "gtk_widget_set_vexpand")
	purego.RegisterLibFunc(&gtkBoxNew, gtk, "gtk_box_new")
	purego.RegisterLibFunc(&gtkBoxAppend, gtk, "gtk_box_append")
	purego.RegisterLibFunc(&gtkWidgetInsertActionGroup, gtk, "gtk_widget_insert_action_group")
	purego.RegisterLibFunc(&gtkPopoverMenuBarNewFromModel, gtk, "gtk_popover_menu_bar_new_from_model")
	purego.RegisterLibFunc(&webkitRegisterHandler3, libWebKit, "webkit_user_content_manager_register_script_message_handler")
}

func (b *gtk4Backend) createWindow() error {
	if !gtkInitCheck0() {
		return errNoDisplay
	}
	b.Platform.window = gtkWindowNew0()
	return nil
}

// buildMenubar builds a GMenu/GAction popover menu bar (GtkMenuBar is gone in
// GTK4). Each non-separator item is wired to a GSimpleAction whose "activate"
// signal dispatches the Go callback stored in gtk4MenuActions.
func (b *gtk4Backend) buildMenubar(menus []Menu) {
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
	gtkWidgetInsertActionGroup(b.Platform.window, "win", group)
	b.Platform.menuBar = gtkPopoverMenuBarNewFromModel(root)
}

func (b *gtk4Backend) showWindow() {
	p := b.Platform
	box := gtkBoxNew(gtkOrientationVertical, 0)
	if p.menuBar != 0 {
		gtkBoxAppend(box, p.menuBar)
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

func (b *gtk4Backend) applySizeHint(width, height int, hint SizeHint) {
	if hint == SizeMax {
		// GTK4 dropped per-axis geometry hints; only default size remains.
		return
	}
	// SizeMin / SizeNone / SizeFixed all map to default size on GTK4.
	gtkWindowSetDefaultSize(b.Platform.window, width, height)
}

func (b *gtk4Backend) savePath(dlg uintptr) string {
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

func (b *gtk4Backend) messageValue(arg uintptr) string {
	cs := jscValueToString(arg)
	s := cstr(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

func (b *gtk4Backend) registerScriptHandler(manager uintptr, name string) {
	webkitRegisterHandler3(manager, name, 0)
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

var (
	gtk4MenuActionMu sync.Mutex
	gtk4MenuActions  = map[uintptr]func(){}
	gtk4MenuActivate uintptr
)
