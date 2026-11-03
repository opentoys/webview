//go:build linux

package linux

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/opentoys/webview/internal/types"
)

var contextBindingsOnce sync.Once
var contextBindingError error
var contextAppend func(uintptr, uintptr)
var contextRemove func(uintptr, uintptr)
var contextGetItems func(uintptr) uintptr
var contextStockAction func(uintptr) int
var contextSeparator func(uintptr) bool
var contextNewSeparator func() uintptr
var contextNewItem func(uintptr, string, uintptr) uintptr
var contextSignal, contextActivate uintptr
var disconnectContextSignal func(uintptr, uint64)
var contextCallbacksMu sync.Mutex
var contextCallbacks = map[uintptr]func(){}
var contextNextID uintptr

func bindContextMenus() error {
	contextBindingsOnce.Do(func() {
		purego.RegisterLibFunc(&disconnectContextSignal, libGObject, "g_signal_handler_disconnect")
		bindings := []struct {
			fn   any
			name string
		}{
			{&contextAppend, "webkit_context_menu_append"}, {&contextRemove, "webkit_context_menu_remove"},
			{&contextGetItems, "webkit_context_menu_get_items"}, {&contextStockAction, "webkit_context_menu_item_get_stock_action"},
			{&contextSeparator, "webkit_context_menu_item_is_separator"}, {&contextNewSeparator, "webkit_context_menu_item_new_separator"},
			{&contextNewItem, "webkit_context_menu_item_new_from_gaction"},
		}
		for _, b := range bindings {
			ptr, err := purego.Dlsym(libWebKit, b.name)
			if err != nil {
				contextBindingError = types.ErrMenuUnsupported
				return
			}
			purego.RegisterFunc(b.fn, ptr)
		}
		contextActivate = purego.NewCallback(func(action, param, data uintptr) {
			contextCallbacksMu.Lock()
			fn := contextCallbacks[data]
			contextCallbacksMu.Unlock()
			if fn != nil {
				fn()
			}
		})
		// WebKitGTK 6.0's context-menu signal has no GdkEvent argument.
		contextSignal = purego.NewCallback(func(view, menu, hitTest, data uintptr) bool {
			if p := lookupPlatform(data); p != nil {
				return p.extendContextMenu(menu)
			}
			return false
		})
	})
	return contextBindingError
}
func (p *gtk) ApplyContextMenu(opts types.ContextMenuConfig) error {
	if err := bindContextMenus(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.extensionsClosed {
		return types.ErrMenuUnsupported
	}
	opts.Items = p.contextActions.Replace(opts.Items)
	p.contextOptions = opts
	return nil
}
func (p *gtk) clearContextCallbacks() {
	contextCallbacksMu.Lock()
	for _, id := range p.contextCallbackIDs {
		delete(contextCallbacks, id)
	}
	p.contextCallbackIDs = nil
	contextCallbacksMu.Unlock()
}
func (p *gtk) extendContextMenu(menu uintptr) bool {
	p.clearContextCallbacks()
	p.mu.Lock()
	opts := p.contextOptions
	closed := p.extensionsClosed
	p.mu.Unlock()
	if closed {
		return false
	}
	// These are WebKitContextMenuAction enum values, independent of locale.
	stock := map[types.MenuItemRole]int{types.MenuItemRoleCopy: 14, types.MenuItemRoleCut: 15, types.MenuItemRolePaste: 16, types.MenuItemRoleSelectAll: 18}
	list := contextGetItems(menu)
	items := make([]uintptr, 0, gListLength(list))
	for i := uint(0); i < uint(gListLength(list)); i++ {
		items = append(items, gListNthData(list, i))
	}
	for _, item := range items {
		if opts.ReplaceDefault {
			contextRemove(menu, item)
			continue
		}
		for _, role := range opts.RemoveDefault {
			if action := stock[role]; action != 0 && contextStockAction(item) == action {
				contextRemove(menu, item)
				break
			}
		}
	}
	if len(opts.Items) > 0 {
		if gListLength(contextGetItems(menu)) > 0 {
			contextAppend(menu, contextNewSeparator())
		}
		for _, item := range opts.Items {
			if item.Separator {
				contextAppend(menu, contextNewSeparator())
				continue
			}
			action := gSimpleActionNew("webview-context", 0)
			contextCallbacksMu.Lock()
			contextNextID++
			id := contextNextID
			contextCallbacks[id] = item.Action
			contextCallbacksMu.Unlock()
			p.contextCallbackIDs = append(p.contextCallbackIDs, id)
			gSignalConnectData(action, "activate", contextActivate, id, 0, 0)
			contextAppend(menu, contextNewItem(action, item.Label, 0))
			gObjectUnref(action)
		}
	}
	list = contextGetItems(menu)
	items = nil
	for i := uint(0); i < uint(gListLength(list)); i++ {
		items = append(items, gListNthData(list, i))
	}
	previous := true
	for _, item := range items {
		separator := contextSeparator(item)
		if separator && previous {
			contextRemove(menu, item)
			continue
		}
		previous = separator
	}
	list = contextGetItems(menu)
	if n := gListLength(list); n > 0 {
		last := gListNthData(list, uint(n-1))
		if contextSeparator(last) {
			contextRemove(menu, last)
		}
	}

	return opts.ReplaceDefault && len(opts.Items) == 0
}
