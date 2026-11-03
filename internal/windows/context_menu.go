//go:build windows

package windows

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/opentoys/webview/internal/types"
)

// Slot numbers follow the published WebView2.h interfaces; inherited slots
// are included (IUnknown occupies 0..2).
func menuCOM(object uintptr, slot int, args ...uintptr) uintptr {
	if object == 0 {
		return 0x80004003
	}
	table := *(*uintptr)(unsafe.Pointer(object))
	method := *(*ComProc)(unsafe.Add(unsafe.Pointer(table), uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	result, _, _ := method.Call(append([]uintptr{object}, args...)...)
	return result
}
func menuQuery(object uintptr, iid string) uintptr {
	guid, _ := NewGUID(iid)
	var result uintptr
	menuCOM(object, 0, uintptr(unsafe.Pointer(guid)), uintptr(unsafe.Pointer(&result)))
	return result
}
func menuOut(object uintptr, slot int) uintptr {
	var result uintptr
	menuCOM(object, slot, uintptr(unsafe.Pointer(&result)))
	return result
}
func menuUint(object uintptr, slot int) uint32 {
	var result uint32
	menuCOM(object, slot, uintptr(unsafe.Pointer(&result)))
	return result
}
func menuString(object uintptr, slot int) string {
	var result *uint16
	menuCOM(object, slot, uintptr(unsafe.Pointer(&result)))
	if result == nil {
		return ""
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(result)))
	return wideToString(result)
}
func removeMenuEvent(object uintptr, slot int, token eventToken) {
	// EventRegistrationToken is a by-value 64-bit struct.
	if unsafe.Sizeof(uintptr(0)) == 4 {
		menuCOM(object, slot, uintptr(uint64(token)), uintptr(uint64(token)>>32))
	} else {
		menuCOM(object, slot, uintptr(token))
	}
}

type nativeMenuEvent struct {
	table *[4]ComProc
	refs  atomic.Int32
	iid   GUID
	fn    func(uintptr)
}

var menuEventsMu sync.Mutex
var menuEvents = map[*nativeMenuEvent]bool{}
var nativeMenuEventTable = [4]ComProc{
	NewComProc(func(self *nativeMenuEvent, iid *GUID, out *uintptr) uintptr {
		if out == nil {
			return 0x80004003
		}
		*out = 0
		unknown := GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
		if *iid != self.iid && *iid != unknown {
			return 0x80004002
		}
		self.refs.Add(1)
		*out = uintptr(unsafe.Pointer(self))
		return 0
	}),
	NewComProc(func(self *nativeMenuEvent) uintptr { return uintptr(self.refs.Add(1)) }),
	NewComProc(func(self *nativeMenuEvent) uintptr {
		refs := self.refs.Add(-1)
		if refs == 0 {
			menuEventsMu.Lock()
			delete(menuEvents, self)
			menuEventsMu.Unlock()
		}
		return uintptr(refs)
	}),
	NewComProc(func(self *nativeMenuEvent, sender, args uintptr) uintptr { self.fn(args); return 0 }),
}

func newNativeMenuEvent(iid string, fn func(uintptr)) *nativeMenuEvent {
	guid, _ := NewGUID(iid)
	event := &nativeMenuEvent{table: &nativeMenuEventTable, iid: *guid, fn: fn}
	event.refs.Store(1)
	menuEventsMu.Lock()
	menuEvents[event] = true
	menuEventsMu.Unlock()
	return event
}
func releaseMenuEvent(event *nativeMenuEvent) {
	if event != nil {
		nativeMenuEventTable[2].Call(uintptr(unsafe.Pointer(event)))
	}
}

type nativeContextItem struct {
	object  uintptr
	token   eventToken
	handler *nativeMenuEvent
}

func (p *Platform) ApplyContextMenu(opts types.ContextMenuConfig) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return types.ErrMenuUnsupported
	}
	if p.ready.Load() == 0 {
		p.contextOptions = opts
		p.contextConfigured = true
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	var err error
	p.MainThread(func() {
		if p.contextView == 0 || p.contextEnv == 0 {
			err = types.ErrMenuUnsupported
			return
		}
		opts.Items = p.contextActions.Replace(opts.Items)
		p.contextOptions = opts
		p.clearContextItems()
	})
	return err
}
func (p *Platform) initializeExtendedMenus() error {
	p.contextView = menuQuery(uintptr(unsafe.Pointer(p.webview)), "0be78e56-c193-4051-b943-23b460c08bdb")
	p.contextEnv = menuQuery(uintptr(unsafe.Pointer(p.env)), "f06f41bf-4b5a-49d8-b9f6-fa16cd29f274")
	if p.contextView != 0 && p.contextEnv != 0 {
		p.contextHandler = newNativeMenuEvent("04d3fe1d-ab87-42fb-a898-da241d35b63c", func(args uintptr) { p.extendContextMenu(args) })
		if menuCOM(p.contextView, 100, uintptr(unsafe.Pointer(p.contextHandler)), uintptr(unsafe.Pointer(&p.contextToken))) != 0 {
			releaseMenuEvent(p.contextHandler)
			p.contextHandler = nil
			menuCOM(p.contextView, 2)
			p.contextView = 0
		}
	}
	if p.contextConfigured && (p.contextView == 0 || p.contextEnv == 0) {
		return types.ErrMenuUnsupported
	}
	p.contextOptions.Items = p.contextActions.Replace(p.contextOptions.Items)
	return p.applySystray()
}
func (p *Platform) clearContextItems() {
	for _, item := range p.contextItems {
		if item.handler != nil {
			removeMenuEvent(item.object, 15, item.token)
			releaseMenuEvent(item.handler)
		}
		menuCOM(item.object, 2)
	}
	p.contextItems = nil
}
func (p *Platform) closeContextMenu() {
	p.contextActions.Close()
	p.clearContextItems()
	if p.contextHandler != nil {
		removeMenuEvent(p.contextView, 101, p.contextToken)
		releaseMenuEvent(p.contextHandler)
		p.contextHandler = nil
	}
	if p.contextView != 0 {
		menuCOM(p.contextView, 2)
		p.contextView = 0
	}
	if p.contextEnv != 0 {
		menuCOM(p.contextEnv, 2)
		p.contextEnv = 0
	}
}
func (p *Platform) filterContextMenu(collection uintptr) {
	names := map[types.MenuItemRole]string{types.MenuItemRoleUndo: "undo", types.MenuItemRoleRedo: "redo", types.MenuItemRoleCut: "cut", types.MenuItemRoleCopy: "copy", types.MenuItemRolePaste: "paste", types.MenuItemRoleSelectAll: "selectAll"}
	count := int(menuUint(collection, 3))
	for i := count - 1; i >= 0; i-- {
		var item uintptr
		if menuCOM(collection, 4, uintptr(i), uintptr(unsafe.Pointer(&item))) != 0 {
			continue
		}
		name := menuString(item, 3)
		remove := false
		for _, role := range p.contextOptions.RemoveDefault {
			if names[role] == name {
				remove = true
				break
			}
		}
		if remove {
			menuCOM(collection, 5, uintptr(i))
		} else if menuUint(item, 8) == 4 {
			children := menuOut(item, 13)
			if children != 0 {
				p.filterContextMenu(children)
				p.normalizeContextMenu(children)
				menuCOM(children, 2)
			}
		}
		menuCOM(item, 2)
	}
}
func (p *Platform) normalizeContextMenu(collection uintptr) {
	previous := true
	for i := uint32(0); i < menuUint(collection, 3); {
		var item uintptr
		if menuCOM(collection, 4, uintptr(i), uintptr(unsafe.Pointer(&item))) != 0 {
			return
		}
		separator := menuUint(item, 8) == 1
		menuCOM(item, 2)
		if separator && previous {
			menuCOM(collection, 5, uintptr(i))
			continue
		}
		previous = separator
		i++
	}
	if count := menuUint(collection, 3); count > 0 {
		var item uintptr
		if menuCOM(collection, 4, uintptr(count-1), uintptr(unsafe.Pointer(&item))) == 0 {
			if menuUint(item, 8) == 1 {
				menuCOM(collection, 5, uintptr(count-1))
			}
			menuCOM(item, 2)
		}
	}

}
func (p *Platform) appendContextItem(collection uintptr, item types.MenuItem) {
	label := utf16PtrFromStr(item.Label)
	kind := uintptr(0)
	if item.Separator {
		kind = 1
	}
	var object uintptr
	result := menuCOM(p.contextEnv, 19, uintptr(unsafe.Pointer(label)), 0, kind, uintptr(unsafe.Pointer(&object)))
	runtime.KeepAlive(label)
	if result != 0 || object == 0 {
		return
	}
	entry := nativeContextItem{object: object}
	if item.Action != nil && !item.Separator {
		entry.handler = newNativeMenuEvent("49e1d0bc-fe9e-4481-b7c2-32324aa21998", func(args uintptr) { item.Action() })
		if menuCOM(object, 14, uintptr(unsafe.Pointer(entry.handler)), uintptr(unsafe.Pointer(&entry.token))) != 0 {
			releaseMenuEvent(entry.handler)
			entry.handler = nil
		}
	}
	menuCOM(collection, 6, uintptr(menuUint(collection, 3)), object)
	p.contextItems = append(p.contextItems, entry)
}
func (p *Platform) extendContextMenu(args uintptr) {
	p.clearContextItems()
	collection := menuOut(args, 3)
	if collection == 0 {
		return
	}
	defer menuCOM(collection, 2)
	if p.contextOptions.ReplaceDefault {
		for count := menuUint(collection, 3); count > 0; count-- {
			menuCOM(collection, 5, uintptr(count-1))
		}
	} else {
		p.filterContextMenu(collection)
	}
	if len(p.contextOptions.Items) > 0 {
		if menuUint(collection, 3) > 0 {
			p.appendContextItem(collection, types.MenuItem{Separator: true})
		}
		for _, item := range p.contextOptions.Items {
			p.appendContextItem(collection, item)
		}
	}
	p.normalizeContextMenu(collection)
	if p.contextOptions.ReplaceDefault && menuUint(collection, 3) == 0 {
		menuCOM(args, 8, 1)
	}
}
