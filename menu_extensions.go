package webview

import (
	"errors"
	"fmt"

	"github.com/opentoys/webview/internal/types"
)

type SystrayOptions = types.SystrayOptions

// ErrMenuUnsupported indicates that the backend or desktop lacks native menus.
var ErrMenuUnsupported = types.ErrMenuUnsupported

type extendedMenus interface {
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

// SetContextMenu replaces the entire native context menu. An empty slice disables it.
func (w *W) SetContextMenu(items []MenuItem) error {
	return w.windowCall(func() error { return w.setContextMenuPublic(items) })
}
func (w *W) setContextMenuPublic(items []MenuItem) error {
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
		return ErrClosed
	}
	if err := validateMenuItems(opts.Items); err != nil {
		return err
	}
	for _, role := range opts.RemoveDefault {
		if role < MenuItemRoleUndo || role > MenuItemRoleSelectAll {
			return fmt.Errorf("webview: unsupported default menu role %d", role)
		}
	}
	opts.Items = w.menuCallbacks(opts.Items)
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
	return w.windowCall(func() error { return w.addContextMenuItemPublic(item) })
}
func (w *W) addContextMenuItemPublic(item MenuItem) error {
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	opts := w.contextMenu
	opts.Items = append(append([]MenuItem(nil), opts.Items...), item)
	return w.setContextMenu(opts)
}

// RemoveContextMenuItem removes an item by ID; a missing ID has no effect.
func (w *W) RemoveContextMenuItem(id string) error {
	return w.windowCall(func() error { return w.removeContextMenuItemPublic(id) })
}
func (w *W) removeContextMenuItemPublic(id string) error {
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
	return w.windowCall(func() error { return w.removeContextMenuItemRolePublic(roles...) })
}
func (w *W) removeContextMenuItemRolePublic(roles ...MenuItemRole) error {
	w.menuMu.Lock()
	defer w.menuMu.Unlock()
	if _, ok := w.p.(extendedMenus); !ok {
		return ErrMenuUnsupported
	}
	if w.menuClosed {
		return ErrClosed
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
