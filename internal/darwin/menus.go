//go:build darwin

package darwin

// Native NSMenu construction and shortcut parsing.

import (
	"strings"

	"github.com/ebitengine/purego/objc"
)

func (p *Platform) applyMenus(menus []Menu) {
	// Clear old callbacks.
	menuCallbackMu.Lock()
	menuCallbacks = map[uintptr]func(){}
	menuCallbackMu.Unlock()

	handler := objc.ID(menuHandlerClass).Send(allocSel).Send(initSel)

	menuBar := objc.ID(nsMenuClass).Send(allocSel).Send(initSel)
	tag := uintptr(1)

	for _, m := range menus {
		topItem := objc.ID(nsMenuItemClass).Send(allocSel)
		topItem = topItem.Send(initWithTitleSel, nsString(m.Label), 0, nsString(""))
		submenu := objc.ID(nsMenuClass).Send(allocSel).Send(initWithTitleOnlySel, nsString(m.Label))
		submenu.Send(autoreleaseSel)
		topItem.Send(setSubmenuSel, submenu)
		menuBar.Send(addItemSel, topItem)

		for _, mi := range m.Items {
			if mi.Separator {
				submenu.Send(addItemSel, objc.ID(nsMenuItemClass).Send(separatorItemSel))
				continue
			}
			item := objc.ID(nsMenuItemClass).Send(allocSel)
			item = item.Send(initWithTitleSel, nsString(mi.Label), menuItemSelectedSel, nsString(""))
			item.Send(setTargetSel, handler)
			item.Send(setTagSel, tag)

			if mi.Shortcut != "" {
				key, mods := parseShortcut(mi.Shortcut)
				if key != "" {
					item.Send(setKeyEquivalentSel, nsString(key))
					item.Send(setKeyEquivalentModifierMaskSel, mods)
				}
			}

			if mi.Action != nil {
				menuCallbackMu.Lock()
				menuCallbacks[tag] = mi.Action
				menuCallbackMu.Unlock()
			}
			tag++

			submenu.Send(addItemSel, item)
		}
	}

	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	app.Send(setMainMenuSel, menuBar)
}

// parseShortcut parses a shortcut string like "Cmd+Shift+Z" into (key, mods).
func parseShortcut(s string) (string, uintptr) {
	var mods uintptr
	key := ""
	for _, part := range strings.Split(s, "+") {
		switch strings.TrimSpace(part) {
		case "Cmd", "Meta":
			mods |= 1 << 20 // NSEventModifierFlagCommand
		case "Shift":
			mods |= 1 << 17 // NSEventModifierFlagShift
		case "Ctrl", "Control":
			mods |= 1 << 18 // NSEventModifierFlagControl
		case "Alt", "Option":
			mods |= 1 << 19 // NSEventModifierFlagOption
		default:
			key = strings.ToLower(strings.TrimSpace(part))
		}
	}
	return key, mods
}

// setupDataStore returns the WKWebsiteDataStore for the platform: a non-
// persistent (in-memory, incognito) store when Incognito is set, else the
// default persistent store. WKWebsiteDataStore has no public initializer and
// the private custom-directory path is unavailable, so DataDir (a Windows/
// Linux concept) is ignored on darwin. Runs on the host thread from setup().
