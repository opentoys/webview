//go:build darwin

package darwin

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/types"
)

// Native objects and tags are owned by the AppKit thread.
type nativeMenu struct {
	menu, handler objc.ID
	tags          []uintptr
}

var nextExtensionTag uintptr = 1 << 32

func (m *nativeMenu) clear() {
	menuCallbackMu.Lock()
	for _, tag := range m.tags {
		delete(menuCallbacks, tag)
	}
	menuCallbackMu.Unlock()
	if m.menu != 0 {
		m.menu.Send(releaseSel)
	}
	if m.handler != 0 {
		m.handler.Send(releaseSel)
	}
	*m = nativeMenu{}
}
func (m *nativeMenu) append(menu objc.ID, items []types.MenuItem) {
	if m.handler == 0 {
		m.handler = objc.ID(menuHandlerClass).Send(allocSel).Send(initSel)
	}
	for _, mi := range items {
		if mi.Separator {
			separator := objc.ID(nsMenuItemClass).Send(separatorItemSel)
			nextExtensionTag++
			separator.Send(setTagSel, nextExtensionTag)
			m.tags = append(m.tags, nextExtensionTag)
			menu.Send(addItemSel, separator)
			continue
		}
		item := objc.ID(nsMenuItemClass).Send(allocSel).Send(initWithTitleSel, nsString(mi.Label), menuItemSelectedSel, nsString(""))
		nextExtensionTag++
		tag := nextExtensionTag
		item.Send(setTargetSel, m.handler)
		item.Send(objc.RegisterName("setRepresentedObject:"), m.handler)
		item.Send(setTagSel, tag)
		if key, mods := parseShortcut(mi.Shortcut); key != "" {
			item.Send(setKeyEquivalentSel, nsString(key))
			item.Send(setKeyEquivalentModifierMaskSel, mods)
		}
		menuCallbackMu.Lock()
		menuCallbacks[tag] = mi.Action
		menuCallbackMu.Unlock()
		m.tags = append(m.tags, tag)
		menu.Send(addItemSel, item)
		item.Send(releaseSel)
	}
}

func (p *Platform) SetSystray(opts types.SystrayOptions) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return types.ErrMenuUnsupported
	}
	p.trayOptions = &opts
	ready := p.window != 0
	p.mu.Unlock()
	if ready {
		var err error
		mainThread(func() { err = p.applySystray() })
		return err
	}
	return nil
}
func (p *Platform) RemoveSystray() error {
	p.mu.Lock()
	p.trayOptions = nil
	p.mu.Unlock()
	mainThread(p.removeSystray)
	return nil
}
func (p *Platform) removeSystray() {
	p.trayActions.Close()
	if p.statusItem != 0 {
		p.statusItem.Send(objc.RegisterName("setMenu:"), objc.ID(0))
		objc.ID(objc.GetClass("NSStatusBar")).Send(objc.RegisterName("systemStatusBar")).Send(objc.RegisterName("removeStatusItem:"), p.statusItem)
		p.statusItem.Send(releaseSel)
		p.statusItem = 0
	}
	p.trayNative.clear()
}
func (p *Platform) applySystray() error {
	p.mu.Lock()
	opts := p.trayOptions
	closed := p.closed
	p.mu.Unlock()
	if closed {
		p.removeSystray()
		return types.ErrMenuUnsupported
	}
	if opts == nil {
		p.removeSystray()
		return nil
	}
	var image objc.ID
	if len(opts.Icon) > 0 {
		data := objc.ID(objc.GetClass("NSData")).Send(objc.RegisterName("dataWithBytes:length:"), unsafe.Pointer(&opts.Icon[0]), uintptr(len(opts.Icon)))
		image = objc.ID(objc.GetClass("NSImage")).Send(allocSel).Send(objc.RegisterName("initWithData:"), data)
		runtime.KeepAlive(opts.Icon)
		if image == 0 {
			return types.ErrMenuUnsupported
		}
		defer image.Send(releaseSel)
		image.Send(objc.RegisterName("setSize:"), cgSize{18, 18})
	} else if opts.Title == "" {
		return types.ErrMenuUnsupported
	}

	if p.statusItem == 0 {
		p.statusItem = objc.ID(objc.GetClass("NSStatusBar")).Send(objc.RegisterName("systemStatusBar")).Send(objc.RegisterName("statusItemWithLength:"), float64(-1)).Send(retainSel)
	}
	if p.statusItem == 0 {
		return types.ErrMenuUnsupported
	}
	button := p.statusItem.Send(objc.RegisterName("button"))
	button.Send(objc.RegisterName("setImage:"), image)
	button.Send(objc.RegisterName("setTitle:"), nsString(opts.Title))
	position := uintptr(1) // NSImageOnly
	if image == 0 {
		position = 0
	} else if opts.Title != "" {
		position = 2
	} // NSNoImage / NSImageLeft

	button.Send(objc.RegisterName("setImagePosition:"), position)
	button.Send(objc.RegisterName("setToolTip:"), nsString(opts.Tooltip))
	var menu nativeMenu
	menu.menu = objc.ID(nsMenuClass).Send(allocSel).Send(initSel)
	menu.append(menu.menu, p.trayActions.Replace(opts.Items))
	p.statusItem.Send(objc.RegisterName("setMenu:"), menu.menu)
	p.trayNative.clear()
	p.trayNative = menu
	return nil
}
func (p *Platform) ApplyContextMenu(opts types.ContextMenuConfig) error {
	// willOpenMenu:withEvent: is the public NSView hook available since macOS 10.11.
	if objc.ID(wkWebViewClass).Send(objc.RegisterName("instancesRespondToSelector:"), objc.RegisterName("willOpenMenu:withEvent:")) == 0 {
		return types.ErrMenuUnsupported
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return types.ErrMenuUnsupported
	}
	opts.Items = p.contextActions.Replace(opts.Items)
	p.contextOptions = opts
	return nil
}
func (p *Platform) extendContextMenu(menu objc.ID) {
	p.mu.Lock()
	opts := p.contextOptions
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	// A view may be notified again for the same native menu. Remove entries
	// owned by the previous extension before appending the current snapshot.
	oldTags := make(map[uintptr]bool, len(p.contextNative.tags))
	for _, tag := range p.contextNative.tags {
		oldTags[tag] = true
	}
	for i := int(menu.Send(objc.RegisterName("numberOfItems"))) - 1; i >= 0; i-- {
		item := menu.Send(objc.RegisterName("itemAtIndex:"), i)
		if oldTags[uintptr(item.Send(objc.RegisterName("tag")))] {
			menu.Send(objc.RegisterName("removeItemAtIndex:"), i)
		}
	}
	p.contextNative.clear()
	if opts.ReplaceDefault {
		menu.Send(objc.RegisterName("removeAllItems"))
	}
	for i := int(menu.Send(objc.RegisterName("numberOfItems"))) - 1; i >= 0; i-- {
		item := menu.Send(objc.RegisterName("itemAtIndex:"), i)
		action := item.Send(objc.RegisterName("action"))
		for _, role := range opts.RemoveDefault {
			matches := action == objc.ID(objc.RegisterName(menuRoleAction(role)))
			// WKWebView forwards default commands through one selector and WebKit's
			// native ContextMenuAction tags, rather than the AppKit responder actions.
			if action == objc.ID(objc.RegisterName("forwardContextMenuAction:")) {
				tags := map[types.MenuItemRole]uintptr{types.MenuItemRoleCopy: 8, types.MenuItemRoleCut: 13, types.MenuItemRolePaste: 14}
				if tag := tags[role]; tag != 0 && uintptr(item.Send(objc.RegisterName("tag"))) == tag {
					matches = true
				}
			}
			if matches {
				menu.Send(objc.RegisterName("removeItemAtIndex:"), i)
				break
			}
		}
	}
	if len(opts.Items) > 0 {
		if menu.Send(objc.RegisterName("numberOfItems")) > 0 {
			p.contextNative.append(menu, []types.MenuItem{{Separator: true}})
		}
		p.contextNative.append(menu, opts.Items)
	}
	// Normalize the native result without changing any remaining actions/targets.
	previousSeparator := true
	for i := 0; i < int(menu.Send(objc.RegisterName("numberOfItems"))); {
		item := menu.Send(objc.RegisterName("itemAtIndex:"), i)
		separator := item.Send(objc.RegisterName("isSeparatorItem")) != 0
		if separator && previousSeparator {
			menu.Send(objc.RegisterName("removeItemAtIndex:"), i)
			continue
		}
		previousSeparator = separator
		i++
	}
	if n := int(menu.Send(objc.RegisterName("numberOfItems"))); n > 0 {
		if menu.Send(objc.RegisterName("itemAtIndex:"), n-1).Send(objc.RegisterName("isSeparatorItem")) != 0 {
			menu.Send(objc.RegisterName("removeItemAtIndex:"), n-1)
		}
	}
}
func (p *Platform) closeExtendedMenus() {
	menuCallbackMu.Lock()
	for _, tag := range p.barTags {
		delete(menuCallbacks, tag)
	}
	p.barTags = nil
	menuCallbackMu.Unlock()

	p.removeSystray()
	p.contextActions.Close()
	p.contextNative.clear()
}
