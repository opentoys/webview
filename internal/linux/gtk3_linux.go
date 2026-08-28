package linux

import "github.com/ebitengine/purego"

// GTK3-specific implementation: classic GtkMenuBar + redraw workarounds.
type gtk3 struct {
	*gtk
}

// newgtk3 wires up the GTK3 version of every function-field hook and registers
// the GTK3-only purego symbols. Returns nil on success; non-nil only if the
// library handle is missing (in which case Run panics downstream).
func newgtk3(p *gtk) error {
	newgtk3symbols()
	p.createWindowFn = pgtk3CreateWindow
	p.buildMenubarFn = pgtk3BuildMenubar
	p.showWindowFn = pgtk3ShowWindow
	p.applySizeHintFn = pgtk3ApplySizeHint
	p.savePathFn = pgtk3SavePath
	p.messageValueFn = pgtk3MessageValue
	p.registerScriptHandlerFn = pgtk3RegisterScriptHandler
	return nil
}

func newgtk3symbols() {
	gtk := libGTK
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
	purego.RegisterLibFunc(&gtkWindowSetPosition, gtk, "gtk_window_set_position")
	// gtk_file_chooser_get_filename (char*) was removed in GTK4; GTK4 returns a
	// GFile via gtk_file_chooser_get_file only.
	purego.RegisterLibFunc(&gtkFileChooserGetFilename, gtk, "gtk_file_chooser_get_filename")
	purego.RegisterLibFunc(&gtkWidgetSetSizeRequest, gtk, "gtk_widget_set_size_request")
	purego.RegisterLibFunc(&webkitUserContentManagerRegisterHandler, libWebKit, "webkit_user_content_manager_register_script_message_handler")
	purego.RegisterLibFunc(&webkitJavascriptResultGetJSValue, libWebKit, "webkit_javascript_result_get_js_value")
}

func pgtk3CreateWindow(p *gtk) error {
	if !gtkInitCheck(0, 0) {
		return errNoDisplay
	}
	p.window = gtkWindowNew(gtkWindowToplevel)
	gtkWindowSetPosition(p.window, 1) // GTK_WIN_POS_CENTER
	return nil
}

var customMenuActionFn uintptr

func pgtk3BuildMenubar(p *gtk, menus []Menu) {
	menuCustomCBMu.Lock()
	menuCustomCBMap = map[uintptr]func(){}
	menuCustomCBMu.Unlock()

	menuBar := gtkMenuBarNew()
	accelGroup := gtkAccelGroupNew()
	gtkWindowAddAccelGroup(p.window, accelGroup)

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

func pgtk3ShowWindow(p *gtk) {
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

func pgtk3ApplySizeHint(p *gtk, width, height int, hint SizeHint) {
	switch hint {
	case SizeMin:
		// Set minimum size; per-axis size-request may be missing on stripped
		// builds, so reuse set_default_size which is present on both.
		gtkWindowSetDefaultSize(p.window, width, height)
	case SizeMax:
		g := gdkGeometry{MaxWidth: int32(width), MaxHeight: int32(height)}
		gtkWindowSetGeometryHints(p.window, 0, &g, gdkHintMaxSize)
	default: // SizeNone, SizeFixed
		gtkWindowResize(p.window, width, height)
	}
}

func pgtk3SavePath(p *gtk, dlg uintptr) string {
	cs := gtkFileChooserGetFilename(dlg) // char*, owned by caller
	if cs == 0 {
		return ""
	}
	defer gFree(cs)
	return cstr(cs)
}

func pgtk3MessageValue(p *gtk, arg uintptr) string {
	cs := jscValueToString(webkitJavascriptResultGetJSValue(arg))
	s := cstr(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

func pgtk3RegisterScriptHandler(p *gtk, manager uintptr, name string) {
	webkitUserContentManagerRegisterHandler(manager, name)
}

// --- GTK3 redraw workarounds (no-compositor ghost popup) --------------------

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
