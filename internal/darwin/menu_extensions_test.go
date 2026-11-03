//go:build darwin

package darwin

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/types"
)

func TestNativeContextMenuExtension(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	called := make(chan string, 4)
	if err = p.ApplyContextMenu(types.ContextMenuConfig{RemoveDefault: []types.MenuItemRole{types.MenuItemRoleCopy}, Items: []MenuItem{{ID: "custom", Label: "Custom", Action: func() { called <- "context" }}}}); err != nil {
		t.Fatal(err)
	}
	var failure string
	mainThread(func() {
		menu := objc.ID(nsMenuClass).Send(allocSel).Send(initSel)
		defer menu.Send(releaseSel)
		add := func(label, action string, tag uintptr) objc.ID {
			item := objc.ID(nsMenuItemClass).Send(allocSel).Send(initWithTitleSel, nsString(label), objc.RegisterName(action), nsString(""))
			item.Send(setTagSel, tag)
			menu.Send(addItemSel, item)
			item.Send(releaseSel)
			return item
		}
		add("Localized Copy", "forwardContextMenuAction:", 8)
		retained := add("Localized Cut", "cut:", 0)
		p.extendContextMenu(menu)
		if menu.Send(objc.RegisterName("numberOfItems")) != 3 {
			failure = "default Copy was not removed or custom item missing"
			return
		}
		if menu.Send(objc.RegisterName("itemAtIndex:"), 0) != retained {
			failure = "remaining default item replaced"
			return
		}
		custom := menu.Send(objc.RegisterName("itemAtIndex:"), 2)
		// Installing the menu bar must not erase extension callbacks.
		p.applyMenus([]Menu{{Label: "Test", Items: []MenuItem{{Label: "Bar", Action: func() { called <- "bar" }}}}})
		p.contextNative.handler.Send(menuItemSelectedSel, custom)
		p.extendContextMenu(menu)
		if menu.Send(objc.RegisterName("numberOfItems")) != 3 {
			failure = "extension appended twice"
		}
	})
	if failure != "" {
		t.Fatal(failure)
	}
	select {
	case value := <-called:
		if value != "context" {
			t.Fatal(value)
		}
	case <-time.After(time.Second):
		t.Fatal("context callback overwritten")
	}
	if err = p.ApplyContextMenu(types.ContextMenuConfig{}); err != nil {
		t.Fatal(err)
	}
	mainThread(func() {
		defer p.closeExtendedMenus()
		menu := objc.ID(nsMenuClass).Send(allocSel).Send(initSel)
		defer menu.Send(releaseSel)
		item := objc.ID(nsMenuItemClass).Send(allocSel).Send(initWithTitleSel, nsString("Cut"), objc.RegisterName("cut:"), nsString(""))
		menu.Send(addItemSel, item)
		item.Send(releaseSel)
		menu.Send(addItemSel, objc.ID(nsMenuItemClass).Send(separatorItemSel))
		menu.Send(addItemSel, objc.ID(nsMenuItemClass).Send(separatorItemSel))
		p.extendContextMenu(menu)
		if menu.Send(objc.RegisterName("numberOfItems")) != 1 {
			t.Error("trailing separator run retained")
		}
	})
}

func TestNativeSystrayLifecycle(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	var data bytes.Buffer
	icon := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			icon.Set(x, y, color.RGBA{60, 140, 230, 255})
		}
	}
	if err := png.Encode(&data, icon); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	opts := types.SystrayOptions{Icon: data.Bytes(), Title: "Visible title", Tooltip: "WebView test", Items: []MenuItem{{ID: "remove", Label: "Remove", Action: func() { done <- p.RemoveSystray() }}}}
	if err := p.SetSystray(opts); err != nil {
		t.Fatal(err)
	}
	var nativeErr error
	var old objc.ID
	mainThread(func() {
		nativeErr = p.applySystray()
		if nativeErr != nil {
			return
		}
		button := p.statusItem.Send(objc.RegisterName("button"))
		if title := goString(button.Send(objc.RegisterName("title"))); title != opts.Title {
			t.Errorf("tray title = %q, want %q", title, opts.Title)
		}
		if position := button.Send(objc.RegisterName("imagePosition")); position != 2 {
			t.Errorf("title image position = %d, want NSImageLeft", position)
		}
		old = p.trayNative.menu.Send(objc.RegisterName("itemAtIndex:"), 0).Send(retainSel)
	})
	if nativeErr != nil {
		t.Fatal(nativeErr)
	}
	defer mainThread(func() { old.Send(releaseSel); p.closeExtendedMenus() })
	opts.Tooltip = "Updated"
	opts.Title = ""
	if err := p.SetSystray(opts); err != nil {
		t.Fatal(err)
	}
	mainThread(func() {
		nativeErr = p.applySystray()
		if title := goString(p.statusItem.Send(objc.RegisterName("button")).Send(objc.RegisterName("title"))); title != "" {
			t.Errorf("tray title was not cleared: %q", title)
		}
		// An old native item retains a valid target, but its removed tag is inert.
		old.Send(objc.RegisterName("target")).Send(menuItemSelectedSel, old)
		current := p.trayNative.menu.Send(objc.RegisterName("itemAtIndex:"), 0)
		p.trayNative.handler.Send(menuItemSelectedSel, current)
	})
	if nativeErr != nil {
		t.Fatal(nativeErr)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback could not remove tray")
	}
	if err := p.RemoveSystray(); err != nil {
		t.Fatal(err)
	}
	mainThread(func() {
		if p.statusItem != 0 || p.trayNative.menu != 0 {
			t.Error("tray not released")
		}
	})
	select {
	case <-done:
		t.Fatal("stale native item triggered callback")
	default:
	}
}

// WebKit may dynamically subclass our WKWebView subclass. Calling SendSuper
// from an inherited override then starts at the wrong class and recurses.
func TestContextMenuHookOnWebViewSubclass(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	if err := p.ApplyContextMenu(types.ContextMenuConfig{Items: []MenuItem{{ID: "hook", Label: "Hook entry"}}}); err != nil {
		t.Fatal(err)
	}
	mainThread(func() {
		previous := activePlatform
		activePlatform = p
		defer func() { p.closeExtendedMenus(); activePlatform = previous }()
		subclass := objc.GetClass("GoWebviewContextMenuHookTestSubclass")
		if subclass == 0 {
			var err error
			subclass, err = objc.RegisterClass("GoWebviewContextMenuHookTestSubclass", fileDropWebViewClass, nil, nil, nil)
			if err != nil {
				t.Error(err)
				return
			}
		}
		config := objc.ID(wkWebViewConfigClass).Send(allocSel).Send(initSel)
		defer config.Send(releaseSel)
		view := objc.ID(subclass).Send(allocSel).Send(initWithFrameSel, rect(0, 0, 100, 100), config)
		if view == 0 {
			t.Error("could not create test WKWebView subclass")
			return
		}
		defer view.Send(releaseSel)
		menu := objc.ID(nsMenuClass).Send(allocSel).Send(initSel)
		defer menu.Send(releaseSel)
		for range 2 {
			// Enter through Objective-C dispatch, rather than calling extendContextMenu
			// directly, so this exercises the inherited override and superclass call.
			view.Send(objc.RegisterName("willOpenMenu:withEvent:"), menu, objc.ID(0))
			if count := menu.Send(objc.RegisterName("numberOfItems")); count != 1 {
				t.Errorf("menu hook produced %d items, want 1", count)
			}
		}
	})
}

func TestNativeContextMenuReplacement(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	mainThread(func() {
		defer p.closeExtendedMenus()
		for _, items := range [][]MenuItem{{{ID: "only", Label: "Only custom"}}, nil} {
			if err := p.ApplyContextMenu(types.ContextMenuConfig{ReplaceDefault: true, Items: items}); err != nil {
				t.Error(err)
				return
			}
			menu := objc.ID(nsMenuClass).Send(allocSel).Send(initSel)
			item := objc.ID(nsMenuItemClass).Send(allocSel).Send(initWithTitleSel, nsString("Default"), objc.RegisterName("copy:"), nsString(""))
			menu.Send(addItemSel, item)
			item.Send(releaseSel)
			p.extendContextMenu(menu)
			if count := int(menu.Send(objc.RegisterName("numberOfItems"))); count != len(items) {
				t.Errorf("replacement has %d items, want %d", count, len(items))
			}
			menu.Send(releaseSel)
		}
	})
}

func TestNativeTitleOnlySystray(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	startAppHost()
	var icon bytes.Buffer
	if err := png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	defer mainThread(p.closeExtendedMenus)
	for _, state := range []struct {
		title    string
		icon     []byte
		position uintptr
	}{
		{"Text only", nil, 0},
		{"With icon", icon.Bytes(), 2},
		{"Text again", nil, 0},
		{"", icon.Bytes(), 1},
	} {
		if err := p.SetSystray(types.SystrayOptions{Title: state.title, Icon: state.icon, Items: []MenuItem{{ID: "test", Label: "Test"}}}); err != nil {
			t.Fatal(err)
		}
		mainThread(func() {
			if err := p.applySystray(); err != nil {
				t.Error(err)
				return
			}
			button := p.statusItem.Send(objc.RegisterName("button"))
			if title := goString(button.Send(objc.RegisterName("title"))); title != state.title {
				t.Errorf("title=%q, want %q", title, state.title)
			}
			hasImage := button.Send(objc.RegisterName("image")) != 0
			if hasImage != (len(state.icon) > 0) {
				t.Errorf("image present=%v, icon bytes=%d", hasImage, len(state.icon))
			}
			if position := button.Send(objc.RegisterName("imagePosition")); uintptr(position) != state.position {
				t.Errorf("image position=%d, want %d", position, state.position)
			}
		})
	}
}
