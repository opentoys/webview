//go:build linux

package linux

import (
	"bytes"
	"fmt"
	"image/png"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
	"github.com/opentoys/webview/internal/types"
)

const trayInterface = "org.kde.StatusNotifierItem"
const menuInterface = "com.canonical.dbusmenu"
const trayPath dbus.ObjectPath = "/StatusNotifierItem"
const trayMenuPath dbus.ObjectPath = "/MenuBar"

type trayPixmap struct {
	Width, Height int32
	Data          []byte
}
type trayLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}
type trayProperty struct {
	ID         int32
	Properties map[string]dbus.Variant
}
type trayEvent struct {
	ID        int32
	Event     string
	Data      dbus.Variant
	Timestamp uint32
}

type linuxTray struct {
	mu       sync.Mutex
	conn     *dbus.Conn
	props    *prop.Properties
	items    []types.MenuItem
	revision uint32
	nextID   int32
	ids      []int32
}

func (t *linuxTray) ContextMenu(x, y int32) *dbus.Error                 { return nil }
func (t *linuxTray) Activate(x, y int32) *dbus.Error                    { return nil }
func (t *linuxTray) SecondaryActivate(x, y int32) *dbus.Error           { return nil }
func (t *linuxTray) Scroll(delta int32, orientation string) *dbus.Error { return nil }
func (t *linuxTray) GetLayout(parent, depth int32, names []string) (uint32, trayLayout, *dbus.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	root := trayLayout{ID: 0, Properties: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}, Children: []dbus.Variant{}}
	for i, item := range t.items {
		node := trayLayout{ID: t.ids[i], Properties: trayItemProperties(item), Children: []dbus.Variant{}}
		if parent == node.ID {
			return t.revision, node, nil
		}
		root.Children = append(root.Children, dbus.MakeVariant(node))
	}
	if parent != 0 {
		return t.revision, trayLayout{}, dbus.NewError("com.canonical.dbusmenu.Error.InvalidMenuItem", []any{"unknown menu item"})
	}
	if depth == 0 {
		root.Children = []dbus.Variant{}
	}
	return t.revision, root, nil
}
func trayItemProperties(item types.MenuItem) map[string]dbus.Variant {
	if item.Separator {
		return map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}
	}
	return map[string]dbus.Variant{"label": dbus.MakeVariant(item.Label), "enabled": dbus.MakeVariant(true), "visible": dbus.MakeVariant(true)}
}
func (t *linuxTray) GetGroupProperties(ids []int32, names []string) ([]trayProperty, *dbus.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := []trayProperty{}
	for i, item := range t.items {
		for _, id := range ids {
			if id == t.ids[i] {
				out = append(out, trayProperty{id, trayItemProperties(item)})
			}
		}
	}
	return out, nil
}
func (t *linuxTray) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, item := range t.items {
		if id == t.ids[i] {
			if v, ok := trayItemProperties(item)[name]; ok {
				return v, nil
			}
		}
	}
	return dbus.MakeVariant(""), nil
}
func (t *linuxTray) Event(id int32, event string, data dbus.Variant, timestamp uint32) *dbus.Error {
	t.mu.Lock()
	var fn func()
	if event == "clicked" {
		for i, item := range t.items {
			if t.ids[i] == id {
				fn = item.Action
				break
			}
		}
	}
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}
func (t *linuxTray) EventGroup(events []trayEvent) ([]int32, *dbus.Error) {
	for _, e := range events {
		t.Event(e.ID, e.Event, e.Data, e.Timestamp)
	}
	return []int32{}, nil
}
func (t *linuxTray) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }
func (t *linuxTray) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}

func newLinuxTray() (*linuxTray, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("%w: session bus: %v", types.ErrMenuUnsupported, err)
	}
	fail := func(err error) (*linuxTray, error) {
		conn.Close()
		return nil, fmt.Errorf("%w: %v", types.ErrMenuUnsupported, err)
	}
	watcher := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	host, err := watcher.GetProperty("org.kde.StatusNotifierWatcher.IsStatusNotifierHostRegistered")
	if err != nil || host.Value() != true {
		return fail(fmt.Errorf("no StatusNotifier host"))
	}
	t := &linuxTray{conn: conn}
	if err = conn.Export(t, trayPath, trayInterface); err != nil {
		return fail(err)
	}
	if err = conn.Export(t, trayMenuPath, menuInterface); err != nil {
		return fail(err)
	}
	conn.Export(introspect.NewIntrospectable(&introspect.Node{Interfaces: []introspect.Interface{{Name: trayInterface, Methods: introspect.Methods(t)}}}), trayPath, "org.freedesktop.DBus.Introspectable")
	conn.Export(introspect.NewIntrospectable(&introspect.Node{Interfaces: []introspect.Interface{{Name: menuInterface, Methods: introspect.Methods(t)}}}), trayMenuPath, "org.freedesktop.DBus.Introspectable")
	properties := map[string]map[string]*prop.Prop{
		trayInterface: {
			"Category": {Value: "ApplicationStatus"}, "Id": {Value: "webview"}, "Title": {Value: ""}, "Status": {Value: "Active"},
			"IconName": {Value: ""}, "IconPixmap": {Value: []trayPixmap{}, Emit: prop.EmitTrue}, "ToolTip": {Value: struct {
				Icon               string
				Pixmaps            []trayPixmap
				Title, Description string
			}{"", []trayPixmap{}, "", ""}, Emit: prop.EmitTrue},
			"ItemIsMenu": {Value: true}, "Menu": {Value: trayMenuPath},
		},
	}
	t.props, err = prop.Export(conn, trayPath, properties)
	if err != nil {
		return fail(err)
	}
	if _, err = prop.Export(conn, trayMenuPath, map[string]map[string]*prop.Prop{menuInterface: {"Version": {Value: uint32(3)}, "TextDirection": {Value: "ltr"}, "Status": {Value: "normal"}, "IconThemePath": {Value: []string{}}}}); err != nil {
		return fail(err)
	}
	if err = watcher.Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, string(trayPath)).Err; err != nil {
		return fail(err)
	}
	return t, nil
}
func (t *linuxTray) update(opts types.SystrayOptions) error {
	image, err := png.Decode(bytes.NewReader(opts.Icon))
	if err != nil {
		return err
	}
	bounds := image.Bounds()
	pixels := make([]byte, 0, bounds.Dx()*bounds.Dy()*4)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := image.At(x, y).RGBA()
			pixels = append(pixels, byte(a>>8), byte(r>>8), byte(g>>8), byte(b>>8))
		}
	}
	pixmaps := []trayPixmap{{int32(bounds.Dx()), int32(bounds.Dy()), pixels}}
	t.mu.Lock()
	t.items = opts.Items
	t.ids = nil
	for range t.items {
		t.nextID++
		t.ids = append(t.ids, t.nextID)
	}
	t.revision++
	revision := t.revision
	t.mu.Unlock()
	t.props.SetMust(trayInterface, "Title", opts.Tooltip)
	t.props.SetMust(trayInterface, "IconPixmap", pixmaps)
	t.props.SetMust(trayInterface, "ToolTip", struct {
		Icon               string
		Pixmaps            []trayPixmap
		Title, Description string
	}{"", pixmaps, opts.Tooltip, ""})
	t.conn.Emit(trayPath, trayInterface+".NewIcon")
	t.conn.Emit(trayPath, trayInterface+".NewToolTip")
	return t.conn.Emit(trayMenuPath, menuInterface+".LayoutUpdated", revision, int32(0))
}
func (p *gtk) SetSystray(opts types.SystrayOptions) error {
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
		p.tray, err = newLinuxTray()
		if err != nil {
			return err
		}
	}
	return p.tray.update(*p.trayOptions)
}
func (p *gtk) RemoveSystray() error {
	p.extensionMu.Lock()
	defer p.extensionMu.Unlock()
	p.trayOptions = nil
	p.trayActions.Close()
	if p.tray != nil {
		p.tray.conn.Close()
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
