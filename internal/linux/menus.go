//go:build linux

package linux

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	gtk4MenuActionMu sync.Mutex
	gtk4MenuActions  = map[uintptr]func(){}
	gtk4MenuActivate uintptr
)

// pgtk4BuildMenubar builds a GMenu/GAction popover menu bar. GtkMenuBar was
// removed in GTK4, so each item is backed by a GSimpleAction.
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
				act := gSimpleActionNew(name, 0)
				gtk4MenuActionMu.Lock()
				gtk4MenuActions[act] = mi.Action
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

// gtk4MenuActivateFn returns the shared menu activation callback.
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

// applyMenubarBorderFix removes the border and shadow around GTK4 popovers.
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
