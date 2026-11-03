package webview

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"runtime"

	"github.com/opentoys/webview/internal/types"
)

type SystrayOptions = types.SystrayOptions

// ErrMenuUnsupported indicates that the backend or desktop lacks native menus.
var ErrMenuUnsupported = types.ErrMenuUnsupported

type extendedMenus interface {
	SetSystray(SystrayOptions) error
	RemoveSystray() error
	ApplyContextMenu(types.ContextMenuConfig) error
}

func validateMenuItems(items []MenuItem) error {
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		if item.ID == "" {
			return errors.New("webview: menu item ID must not be empty")
		}
		if ids[item.ID] {
			return fmt.Errorf("webview: duplicate menu item ID %q", item.ID)
		}
		ids[item.ID] = true
	}
	return nil
}

// SetSystray creates or replaces the native tray icon and menu, before or during Run.
func (w *W) SetSystray(opts SystrayOptions) error {
	p, ok := w.p.(extendedMenus)
	if !ok {
		return ErrMenuUnsupported
	}
	if err := validateMenuItems(opts.Items); err != nil {
		return err
	}
	if len(opts.Icon) == 0 {
		if opts.Title == "" {
			return errors.New("webview: systray requires an icon or title")
		}
		if runtime.GOOS != "darwin" {
			return ErrMenuUnsupported
		}
	} else if _, err := png.Decode(bytes.NewReader(opts.Icon)); err != nil {
		return fmt.Errorf("webview: systray PNG: %w", err)
	}
	opts.Icon = append([]byte(nil), opts.Icon...)
	opts.Items = append([]MenuItem(nil), opts.Items...)
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	if w.menuClosed {
		return errors.New("webview: menu update after Close")
	}
	return p.SetSystray(opts)
}

// RemoveSystray removes the tray icon. Repeated calls are harmless.
func (w *W) RemoveSystray() error {
	p, ok := w.p.(extendedMenus)
	if !ok {
		return ErrMenuUnsupported
	}
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	return p.RemoveSystray()
}

// SetContextMenu replaces the entire native context menu. An empty slice disables it.
func (w *W) SetContextMenu(items []MenuItem) error {
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	return w.setContextMenu(types.ContextMenuConfig{ReplaceDefault: true, Items: items})
}
func (w *W) setContextMenu(opts types.ContextMenuConfig) error {
	p, ok := w.p.(extendedMenus)
	if !ok {
		return ErrMenuUnsupported
	}
	if w.menuClosed {
		return errors.New("webview: menu update after Close")
	}
	if err := validateMenuItems(opts.Items); err != nil {
		return err
	}
	for _, role := range opts.RemoveDefault {
		if role < MenuItemRoleUndo || role > MenuItemRoleSelectAll {
			return fmt.Errorf("webview: unsupported default menu role %d", role)
		}
	}
	opts.Items = append([]MenuItem(nil), opts.Items...)
	opts.RemoveDefault = append([]MenuItemRole(nil), opts.RemoveDefault...)
	if err := p.ApplyContextMenu(opts); err != nil {
		return err
	}
	w.contextMenu = opts
	return nil
}

// AddContextMenuItem appends one uniquely identified custom item. Browser defaults
// are preserved until SetContextMenu replaces the menu.
func (w *W) AddContextMenuItem(item MenuItem) error {
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	opts := w.contextMenu
	opts.Items = append(append([]MenuItem(nil), opts.Items...), item)
	return w.setContextMenu(opts)
}

// RemoveContextMenuItem removes an item by ID; a missing ID has no effect.
func (w *W) RemoveContextMenuItem(id string) error {
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	if _, ok := w.p.(extendedMenus); !ok {
		return ErrMenuUnsupported
	}
	opts := w.contextMenu
	for i, item := range opts.Items {
		if item.ID == id {
			opts.Items = append(append([]MenuItem(nil), opts.Items[:i]...), opts.Items[i+1:]...)
			return w.setContextMenu(opts)
		}
	}
	return nil
}

// RemoveContextMenuItemRole hides default actions and removes custom items with
// matching roles. Missing roles are ignored; None and unknown roles are invalid.
func (w *W) RemoveContextMenuItemRole(roles ...MenuItemRole) error {
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	if _, ok := w.p.(extendedMenus); !ok {
		return ErrMenuUnsupported
	}
	if w.menuClosed {
		return errors.New("webview: menu update after Close")
	}
	if len(roles) == 0 {
		return nil
	}
	remove := make(map[MenuItemRole]bool, len(roles))
	for _, role := range roles {
		if role < MenuItemRoleUndo || role > MenuItemRoleSelectAll {
			return fmt.Errorf("webview: unsupported menu role %d", role)
		}
		remove[role] = true
	}
	opts := w.contextMenu
	opts.Items = make([]MenuItem, 0, len(w.contextMenu.Items))
	for _, item := range w.contextMenu.Items {
		if !remove[item.Role] {
			opts.Items = append(opts.Items, item)
		}
	}
	opts.RemoveDefault = append([]MenuItemRole(nil), opts.RemoveDefault...)
	for _, role := range opts.RemoveDefault {
		delete(remove, role)
	}
	for _, role := range roles {
		if remove[role] {
			opts.RemoveDefault = append(opts.RemoveDefault, role)
			delete(remove, role)
		}
	}
	return w.setContextMenu(opts)
}
