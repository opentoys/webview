//go:build linux

package linux

import (
	"bytes"
	"fmt"
	"image/png"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/opentoys/webview/internal/types"
)

const trayInterface = "org.kde.StatusNotifierItem"
const menuInterface = "com.canonical.dbusmenu"
const trayPath = "/StatusNotifierItem"
const trayMenuPath = "/MenuBar"
const invalidMenuItem = "com.canonical.dbusmenu.Error.InvalidMenuItem"

type trayPixmap struct {
	Width, Height int32
	Data          []byte
}

// Mutable native state belongs exclusively to trayBus's context thread.
type linuxTray struct {
	conn     uintptr
	id       uintptr
	objects  []uint32
	node     uintptr
	closed   bool
	opts     types.SystrayOptions
	pixmap   trayPixmap
	revision uint32
	nextID   int32
	ids      []int32
}

var trayCallbacksOnce sync.Once
var trayVTable dbusVTable

func initTrayCallbacks() {
	trayCallbacksOnce.Do(func() {
		trayVTable.Method = purego.NewCallback(func(conn, sender, path, iface, method, params, invocation, data uintptr) {
			t := trayBus.trays[data]
			if t == nil || t.closed {
				gio.ReturnError(invocation, "org.freedesktop.DBus.Error.UnknownObject", "tray is closed")
				return
			}
			reply, callbacks, errName := t.method(nativeString(iface), nativeString(method), params)
			if errName != "" {
				gio.ReturnError(invocation, errName, "unknown method, property or menu item")
				return
			}
			gio.Return(invocation, reply)
			// Return queues the reply. Flush it before a user callback can
			// close this connection, otherwise Close may discard the reply.
			// The flush runs outside the context, with its own native reference.
			if len(callbacks) > 0 {
				gio.ObjectRef(conn)
				go func() {
					defer gio.ObjectUnref(conn)
					var e uintptr
					if gio.Flush(conn, 0, &e) == 0 {
						if e != 0 {
							gio.ErrorFree(e)
						}
						return
					}
					for _, fn := range callbacks {
						if fn != nil {
							go fn()
						}
					}
				}()
			}
		})
		trayVTable.Get = purego.NewCallback(func(conn, sender, path, iface, property, errorOut, data uintptr) uintptr {
			t := trayBus.trays[data]
			if t == nil || t.closed {
				return 0
			}
			return t.property(nativeString(iface), nativeString(property))
		})
	})
}
func decodeTrayPixmap(icon []byte) (trayPixmap, error) {
	image, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		return trayPixmap{}, err
	}
	bounds := image.Bounds()
	pixels := make([]byte, 0, bounds.Dx()*bounds.Dy()*4)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := image.At(x, y).RGBA()
			pixels = append(pixels, byte(a>>8), byte(r>>8), byte(g>>8), byte(b>>8))
		}
	}
	return trayPixmap{int32(bounds.Dx()), int32(bounds.Dy()), pixels}, nil
}
func (p trayPixmap) variant() uintptr {
	return vArray("(iiay)", vTuple(gio.NewInt(p.Width), gio.NewInt(p.Height),
		gio.NewFixedArray("y", unsafe.SliceData(p.Data), uintptr(len(p.Data)), 1)))
}
func newLinuxTray(opts types.SystrayOptions) (*linuxTray, error) {
	pixmap, err := decodeTrayPixmap(opts.Icon)
	if err != nil {
		return nil, err
	}
	if err = trayBus.start(); err != nil {
		return nil, err
	}
	conn, err := connectSession()
	if err != nil {
		return nil, fmt.Errorf("%w: session bus: %v", types.ErrMenuUnsupported, err)
	}
	// This reference protects outbound calls even when StopApplication closes
	// the tray concurrently. The context owns a separate reference.
	defer gio.ObjectUnref(conn)
	t := &linuxTray{}
	err = trayBus.do(func() error {
		initTrayCallbacks()
		t.conn = gio.ObjectRef(conn)
		trayBus.nextID++
		t.id = trayBus.nextID
		trayBus.trays[t.id] = t
		t.publish(opts, pixmap)
		var e uintptr
		t.node = gio.NodeNew(trayXML, &e)
		if t.node == 0 {
			t.closeNative()
			return takeGError(e)
		}
		for _, obj := range []struct{ path, iface string }{{trayPath, trayInterface}, {trayMenuPath, menuInterface}} {
			id := gio.Register(conn, obj.path, gio.NodeLookup(t.node, obj.iface), &trayVTable, t.id, 0, &e)
			if id == 0 {
				t.closeNative()
				return takeGError(e)
			}
			t.objects = append(t.objects, id)
		}
		return nil
	})
	if err != nil {
		// No context owns this connection if dispatch was rejected during shutdown.
		var e uintptr
		gio.Close(conn, 0, &e)
		if e != 0 {
			gio.ErrorFree(e)
		}
		return nil, fmt.Errorf("%w: %v", types.ErrMenuUnsupported, err)
	}
	fail := func(err error) (*linuxTray, error) {
		_ = t.close()
		return nil, fmt.Errorf("%w: %v", types.ErrMenuUnsupported, err)
	}
	const watcher = "org.kde.StatusNotifierWatcher"
	v, err := dbusCall(conn, watcher, "/StatusNotifierWatcher", "org.freedesktop.DBus.Properties", "Get", vTuple(gio.NewString(watcher), gio.NewString("IsStatusNotifierHostRegistered")))
	if err != nil {
		return fail(err)
	}
	if nativeString(gio.Signature(v)) != "(v)" {
		gio.Unref(v)
		return fail(fmt.Errorf("invalid StatusNotifier host property response"))
	}
	boxed := gio.Child(v, 0)
	value := gio.Variant(boxed)
	host := nativeString(gio.Signature(value)) == "b" && gio.Bool(value) != 0
	gio.Unref(value)
	gio.Unref(boxed)
	gio.Unref(v)
	if !host {
		return fail(fmt.Errorf("no StatusNotifier host"))
	}
	v, err = dbusCall(conn, watcher, "/StatusNotifierWatcher", watcher, "RegisterStatusNotifierItem", vTuple(gio.NewString(trayPath)))
	if err != nil {
		return fail(err)
	}
	if nativeString(gio.Signature(v)) != "()" {
		gio.Unref(v)
		return fail(fmt.Errorf("invalid StatusNotifier registration response"))
	}
	gio.Unref(v)
	// Detect shutdown while the outbound registration was in flight.
	if err = trayBus.do(func() error {
		if t.closed {
			return errTrayClosed
		}
		return nil
	}); err != nil {
		return fail(err)
	}
	return t, nil
}
func (t *linuxTray) publish(opts types.SystrayOptions, pixmap trayPixmap) {
	t.opts = opts
	t.opts.Items = append([]types.MenuItem(nil), opts.Items...)
	t.pixmap = pixmap
	t.ids = nil
	for range opts.Items {
		t.nextID++
		t.ids = append(t.ids, t.nextID)
	}
	t.revision++
}
func (t *linuxTray) update(opts types.SystrayOptions) error {
	pixmap, err := decodeTrayPixmap(opts.Icon)
	if err != nil {
		return err
	}
	return trayBus.do(func() error {
		if t.closed {
			return errTrayClosed
		}
		t.publish(opts, pixmap)
		changed := []uintptr{}
		for _, name := range []string{"ItemIsMenu", "Title", "IconPixmap", "ToolTip"} {
			changed = append(changed, gio.NewEntry(gio.NewString(name), gio.NewVariant(t.property(trayInterface, name))))
		}
		if err := t.emit(trayPath, "org.freedesktop.DBus.Properties", "PropertiesChanged", vTuple(gio.NewString(trayInterface), vArray("{sv}", changed...), vStrings(nil))); err != nil {
			return err
		}
		for _, signal := range []string{"NewIcon", "NewToolTip", "NewTitle"} {
			if err := t.emit(trayPath, trayInterface, signal, vTuple()); err != nil {
				return err
			}
		}
		return t.emit(trayMenuPath, menuInterface, "LayoutUpdated", vTuple(gio.NewUint(t.revision), gio.NewInt(0)))
	})
}
func (t *linuxTray) emit(path, iface, name string, args uintptr) error {
	var e uintptr
	if gio.Emit(t.conn, 0, path, iface, name, args, &e) == 0 {
		return takeGError(e)
	}
	return nil
}
func (t *linuxTray) closeNative() {
	if t.closed {
		return
	}
	t.closed = true
	for _, id := range t.objects {
		gio.Unregister(t.conn, id)
	}
	t.objects = nil
	if t.node != 0 {
		gio.NodeUnref(t.node)
		t.node = 0
	}
	if t.conn != 0 {
		var e uintptr
		gio.Close(t.conn, 0, &e)
		if e != 0 {
			gio.ErrorFree(e)
		}
		gio.ObjectUnref(t.conn)
		t.conn = 0
	}
	t.opts = types.SystrayOptions{}
	t.ids = nil
	t.pixmap = trayPixmap{}
	delete(trayBus.trays, t.id)
}
func (t *linuxTray) close() error {
	err := trayBus.do(func() error { t.closeNative(); return nil })
	if err == errBusStopped {
		// stop drains every registered tray before signaling completion.
		trayBus.mu.Lock()
		done := trayBus.done
		trayBus.mu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	return err
}

func (p *gtk) SetSystray(opts types.SystrayOptions) error {
	if len(opts.Icon) == 0 {
		return types.ErrMenuUnsupported
	}
	p.extensionMu.Lock()
	defer p.extensionMu.Unlock()
	p.mu.Lock()
	closed := p.extensionsClosed
	ready := p.uiTID.Load() != 0
	p.mu.Unlock()
	if closed {
		return types.ErrMenuUnsupported
	}
	opts.Items = p.trayActions.Replace(opts.Items)
	p.trayOptions = &opts
	if !ready {
		return nil
	}
	return p.applySystray()
}
func (p *gtk) applySystray() error {
	if p.trayOptions == nil {
		return nil
	}
	if p.tray == nil {
		var err error
		p.tray, err = newLinuxTray(*p.trayOptions)
		return err
	}
	return p.tray.update(*p.trayOptions)
}
func (p *gtk) RemoveSystray() error {
	p.extensionMu.Lock()
	defer p.extensionMu.Unlock()
	p.trayOptions = nil
	p.trayActions.Close()
	if p.tray != nil {
		p.tray.close()
		p.tray = nil
	}
	return nil
}
func (p *gtk) closeExtendedMenus() {
	if p.contextSignalID != 0 && p.webview != 0 {
		disconnectContextSignal(p.webview, p.contextSignalID)
		p.contextSignalID = 0
	}
	p.clearContextCallbacks()

	p.mu.Lock()
	p.extensionsClosed = true
	p.contextActions.Close()
	p.mu.Unlock()
	p.RemoveSystray()
}
