//go:build linux

package linux

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/opentoys/webview/internal/types"
)

func requireGIO(t *testing.T) {
	t.Helper()
	if err := loadGIO(); err != nil {
		t.Fatal(err)
	}
}
func testIcon(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestGVariant(t *testing.T) {
	requireGIO(t)
	cases := []struct {
		name, sig string
		make      func() uintptr
		check     func(uintptr) bool
	}{
		{"string", "s", func() uintptr { return gio.NewString("你好") }, func(v uintptr) bool { return vReadString(v) == "你好" }},
		{"path", "o", func() uintptr { return gio.NewPath(trayMenuPath) }, func(v uintptr) bool { return vReadString(v) == trayMenuPath }},
		{"bool", "b", func() uintptr { return vBool(true) }, func(v uintptr) bool { return gio.Bool(v) == 1 }},
		{"int", "i", func() uintptr { return gio.NewInt(-42) }, func(v uintptr) bool { return gio.Int(v) == -42 }},
		{"uint", "u", func() uintptr { return gio.NewUint(42) }, func(v uintptr) bool { return gio.Uint(v) == 42 }},
		{"strings", "as", func() uintptr { return vStrings([]string{"a", "b"}) }, func(v uintptr) bool { return strings.Join(vReadStrings(v), ",") == "a,b" }},
		{"empty", "ai", func() uintptr { return vInts(nil) }, func(v uintptr) bool { return gio.Count(v) == 0 }},
		{"tuple", "(is)", func() uintptr { return vTuple(gio.NewInt(7), gio.NewString("x")) }, func(v uintptr) bool { return vChildInt(v, 0) == 7 && vChildString(v, 1) == "x" }},
		{"variant", "v", func() uintptr { return gio.NewVariant(gio.NewString("boxed")) }, func(v uintptr) bool { c := gio.Variant(v); defer gio.Unref(c); return vReadString(c) == "boxed" }},
		{"dict", "a{sv}", func() uintptr {
			return vProperties(map[string]uintptr{"a": gio.NewString("b"), "c": vBool(true)}, []string{"a"})
		}, func(v uintptr) bool { return gio.Count(v) == 1 }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			v := gio.RefSink(tt.make())
			defer gio.Unref(v)
			if got := nativeString(gio.Signature(v)); got != tt.sig {
				t.Fatalf("signature %s != %s", got, tt.sig)
			}
			if !tt.check(v) {
				t.Fatal("round trip failed")
			}
		})
	}
}
func TestTrayEncoding(t *testing.T) {
	requireGIO(t)
	p, err := decodeTrayPixmap(testIcon(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 1 || p.Height != 1 || !bytes.Equal(p.Data, []byte{255, 20, 40, 60}) {
		t.Fatalf("pixmap: %+v", p)
	}
	if _, err := decodeTrayPixmap([]byte("bad PNG")); err == nil {
		t.Fatal("invalid PNG accepted")
	}
	v := gio.RefSink(p.variant())
	defer gio.Unref(v)
	if nativeString(gio.Signature(v)) != "a(iiay)" {
		t.Fatal("pixmap signature")
	}
	tray := &linuxTray{}
	tray.publish(types.SystrayOptions{Items: []types.MenuItem{{Label: "Open"}, {Separator: true}}}, p)
	layout := gio.RefSink(tray.layout(0, -1, nil))
	defer gio.Unref(layout)
	if nativeString(gio.Signature(layout)) != "(ia{sv}av)" {
		t.Fatal("layout signature")
	}
	children := gio.Child(layout, 2)
	defer gio.Unref(children)
	if gio.Count(children) != 2 {
		t.Fatal("missing menu children")
	}
	shallow := gio.RefSink(tray.layout(0, 0, nil))
	defer gio.Unref(shallow)
	c := gio.Child(shallow, 2)
	defer gio.Unref(c)
	if gio.Count(c) != 0 {
		t.Fatal("depth ignored")
	}
	old := tray.ids[0]
	tray.publish(types.SystrayOptions{Items: []types.MenuItem{{Label: "New"}}}, p)
	if tray.ids[0] == old {
		t.Fatal("menu IDs reused")
	}
	args := gio.RefSink(vTuple(gio.NewInt(old), gio.NewString("label")))
	defer gio.Unref(args)
	if _, _, errName := tray.method(menuInterface, "GetProperty", args); errName != invalidMenuItem {
		t.Fatal(errName)
	}
	var e uintptr
	if n := gio.NodeNew("<invalid", &e); n != 0 {
		gio.NodeUnref(n)
		t.Fatal("invalid XML accepted")
	}
	if err := takeGError(e); err == nil || err.Error() == "" {
		t.Fatal("GError not converted")
	}
}

// The watcher runs on the same private context as the trays. Its registration
// handler queries the tray before answering, exposing context-thread deadlocks.
func mockWatcher(t *testing.T) (uintptr, <-chan string) {
	t.Helper()
	requireGIO(t)
	if err := trayBus.start(); err != nil {
		t.Fatal(err)
	}
	conn, err := connectSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var e uintptr
		gio.Close(conn, 0, &e)
		if e != 0 {
			gio.ErrorFree(e)
		}
		gio.ObjectUnref(conn)
	})
	name, err := dbusCall(conn, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", vTuple(gio.NewString("org.kde.StatusNotifierWatcher"), gio.NewUint(0)))
	if err != nil {
		t.Fatal(err)
	}
	result := gio.Child(name, 0)
	code := gio.Uint(result)
	gio.Unref(result)
	gio.Unref(name)
	if code != 1 {
		t.Fatalf("watcher name unavailable: %d", code)
	}
	seen := make(chan string, 16)
	mode := os.Getenv("WEBVIEW_DBUS_FAIL_MODE")
	table := dbusVTable{
		Get: purego.NewCallback(func(conn, sender, path, iface, property, e, data uintptr) uintptr { return vBool(mode != "no-host") }),
		Method: purego.NewCallback(func(c, sender, path, iface, method, p, invocation, data uintptr) {
			if mode == "reject" {
				gio.ReturnError(invocation, "org.test.Rejected", "registration rejected")
				return
			}
			dest := nativeString(sender)
			gio.ObjectRef(invocation)
			go func() {
				defer gio.ObjectUnref(invocation)
				v, err := dbusCall(conn, dest, trayPath, "org.freedesktop.DBus.Properties", "GetAll", vTuple(gio.NewString(trayInterface)))
				if err != nil {
					gio.ReturnError(invocation, "org.test.QueryFailed", err.Error())
					return
				}
				gio.Unref(v)
				gio.Return(invocation, vTuple())
				seen <- dest
			}()
		}),
	}
	var node uintptr
	var id uint32
	err = trayBus.do(func() error {
		var e uintptr
		node = gio.NodeNew(`<node><interface name="org.kde.StatusNotifierWatcher"><property name="IsStatusNotifierHostRegistered" type="b" access="read"/><method name="RegisterStatusNotifierItem"><arg type="s" direction="in"/></method></interface></node>`, &e)
		if node == 0 {
			return takeGError(e)
		}
		id = gio.Register(conn, "/StatusNotifierWatcher", gio.NodeLookup(node, "org.kde.StatusNotifierWatcher"), &table, 0, 0, &e)
		if id == 0 {
			gio.NodeUnref(node)
			return takeGError(e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = trayBus.do(func() error { gio.Unregister(conn, id); gio.NodeUnref(node); return nil }) })
	return conn, seen
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(8 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}
func TestGDBusSession(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("run with dbus-run-session")
	}
	conn, seen := mockWatcher(t)
	opts := types.SystrayOptions{Icon: testIcon(t), Tooltip: "one", Items: []types.MenuItem{{Label: "Open"}}}
	first, err := NewTray(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	dest := receive(t, seen)
	second, err := NewTray(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	dest2 := receive(t, seen)
	if dest == dest2 {
		t.Fatal("trays share connection")
	}
	call := func(destination, path, iface, method string, args uintptr) uintptr {
		t.Helper()
		v, err := dbusCall(conn, destination, path, iface, method, args)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := call(dest, trayMenuPath, menuInterface, "GetLayout", vTuple(gio.NewInt(0), gio.NewInt(-1), vStrings(nil)))
	if nativeString(gio.Signature(v)) != "(u(ia{sv}av))" {
		t.Fatal("wire layout signature")
	}
	gio.Unref(v)
	for _, tc := range []struct {
		method string
		args   uintptr
		want   string
	}{
		{"Missing", vTuple(), "UnknownMethod"},
		{"GetLayout", vTuple(gio.NewString("wrong")), "InvalidArgs"},
		{"GetProperty", vTuple(gio.NewInt(-99), gio.NewString("label")), "InvalidMenuItem"},
	} {
		v, err := dbusCall(conn, dest, trayMenuPath, menuInterface, tc.method, tc.args)
		if v != 0 {
			gio.Unref(v)
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.method, err)
		}
	}
	// Subscribe through a separate connection and observe actual wire signals.
	signals := make(chan string, 16)
	h, err := purego.Dlopen("libgio-2.0.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Fatal(err)
	}
	defer purego.Dlclose(h)
	var subscribe func(uintptr, string, uintptr, uintptr, uintptr, uintptr, uint32, uintptr, uintptr, uintptr) uint32
	var unsubscribe func(uintptr, uint32)
	purego.RegisterLibFunc(&subscribe, h, "g_dbus_connection_signal_subscribe")
	purego.RegisterLibFunc(&unsubscribe, h, "g_dbus_connection_signal_unsubscribe")
	cb := purego.NewCallback(func(c, sender, path, iface, signal, params, data uintptr) { signals <- nativeString(signal) })
	var subscription uint32
	if err := trayBus.do(func() error { subscription = subscribe(conn, dest, 0, 0, 0, 0, 0, cb, 0, 0); return nil }); err != nil {
		t.Fatal(err)
	}

	// Round-trip on this connection ensures the bus processed AddMatch.
	barrier := call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "GetId", vTuple())
	gio.Unref(barrier)
	if err := first.Update(opts); err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"PropertiesChanged": false, "NewIcon": false, "NewToolTip": false, "NewTitle": false, "LayoutUpdated": false}
	for range wanted {
		wanted[receive(t, signals)] = true
	}
	for name, received := range wanted {
		if !received {
			t.Fatalf("missing signal %s", name)
		}
	}
	// Only this update needs signal observation.
	if err := trayBus.do(func() error { unsubscribe(conn, subscription); return nil }); err != nil {
		t.Fatal(err)
	}
	// Exercise every menu method with its actual wire signature.
	selected := make(chan struct{}, 2)
	opts.Items[0].Action = func() { selected <- struct{}{} }
	if err := first.Update(opts); err != nil {
		t.Fatal(err)
	}
	v = call(dest, trayMenuPath, menuInterface, "GetLayout", vTuple(gio.NewInt(0), gio.NewInt(-1), vStrings([]string{"label"})))
	root := gio.Child(v, 1)
	children := gio.Child(root, 2)
	boxed := gio.Child(children, 0)
	node := gio.Variant(boxed)
	itemID := vChildInt(node, 0)
	gio.Unref(node)
	gio.Unref(boxed)
	gio.Unref(children)
	gio.Unref(root)
	gio.Unref(v)
	for _, tc := range []struct {
		method, sig string
		args        uintptr
	}{
		{"GetGroupProperties", "(a(ia{sv}))", vTuple(vInts(nil), vStrings([]string{"label"}))},
		{"GetProperty", "(v)", vTuple(gio.NewInt(itemID), gio.NewString("label"))},
		{"AboutToShow", "(b)", vTuple(gio.NewInt(itemID))},
		{"AboutToShowGroup", "(aiai)", vTuple(vInts([]int32{itemID, -1}))},
		{"Event", "()", vTuple(gio.NewInt(itemID), gio.NewString("clicked"), gio.NewVariant(gio.NewInt(0)), gio.NewUint(0))},
		{"EventGroup", "(ai)", vTuple(vArray("(isvu)", vTuple(gio.NewInt(itemID), gio.NewString("clicked"), gio.NewVariant(gio.NewInt(0)), gio.NewUint(0)), vTuple(gio.NewInt(-1), gio.NewString("clicked"), gio.NewVariant(gio.NewInt(0)), gio.NewUint(0))))},
	} {
		v = call(dest, trayMenuPath, menuInterface, tc.method, tc.args)
		if nativeString(gio.Signature(v)) != tc.sig {
			t.Fatalf("%s signature", tc.method)
		}
		gio.Unref(v)
	}
	receive(t, selected)
	receive(t, selected)
	// A blocked user callback must not delay the D-Bus response or property reads.
	entered := make(chan struct{})
	release := make(chan struct{})
	opts.OnClick = func() { close(entered); <-release }
	if err := first.Update(opts); err != nil {
		t.Fatal(err)
	}
	v = call(dest, trayPath, trayInterface, "Activate", vTuple(gio.NewInt(0), gio.NewInt(0)))
	gio.Unref(v)
	receive(t, entered)
	v = call(dest, trayPath, "org.freedesktop.DBus.Properties", "Get", vTuple(gio.NewString(trayInterface), gio.NewString("Title")))
	gio.Unref(v)
	close(release)
	// Reentrant update and close execute on ordinary Go goroutines.
	completed := make(chan error, 1)
	opts.OnClick = func() {
		if err := first.Update(opts); err != nil {
			completed <- err
			return
		}
		completed <- first.Close()
	}
	if err := first.Update(opts); err != nil {
		t.Fatal(err)
	}
	v = call(dest, trayPath, trayInterface, "Activate", vTuple(gio.NewInt(0), gio.NewInt(0)))
	gio.Unref(v)
	if err := receive(t, completed); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	v = call(dest2, trayPath, "org.freedesktop.DBus.Properties", "GetAll", vTuple(gio.NewString(trayInterface)))
	gio.Unref(v)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = second.Update(opts); _ = second.Close() }()
	}
	wg.Wait()
	// Registration failure leaves no exported objects or connection references.
	if err := trayBus.do(func() error {
		if len(trayBus.trays) != 0 {
			return errors.New("tray registry leaked")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestGDBusStopHelper(t *testing.T) {
	if os.Getenv("WEBVIEW_DBUS_STOP_HELPER") != "1" {
		return
	}
	conn, seen := mockWatcher(t)
	opts := types.SystrayOptions{Icon: testIcon(t)}
	tray, err := NewTray(opts)
	if err != nil {
		t.Fatal(err)
	}
	dest := receive(t, seen)
	completed := make(chan error, 1)
	opts.OnClick = func() { completed <- StopApplication() }
	if err := tray.Update(opts); err != nil {
		t.Fatal(err)
	}
	v, err := dbusCall(conn, dest, trayPath, trayInterface, "Activate", vTuple(gio.NewInt(0), gio.NewInt(0)))
	if err != nil {
		t.Fatal(err)
	}
	gio.Unref(v)
	if err := receive(t, completed); err != nil {
		t.Fatal(err)
	}
	if err := tray.Close(); err != nil {
		t.Fatal(err)
	}
	if gtkLib != 0 {
		t.Fatal("tray initialized GTK")
	}
	if err := trayBus.do(func() error { return nil }); !errors.Is(err, types.ErrMenuUnsupported) {
		t.Fatalf("stopped dispatch: %v", err)
	}
}
func TestGDBusStop(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("run with dbus-run-session")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dbus-run-session", "--", exe, "-test.run=^TestGDBusStopHelper$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "WEBVIEW_DBUS_STOP_HELPER=1", "DISPLAY=", "WAYLAND_DISPLAY=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
func TestMissingSession(t *testing.T) {
	requireGIO(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/no/such/webview-bus")
	_, err := NewTray(types.SystrayOptions{Icon: testIcon(t)})
	if !errors.Is(err, types.ErrMenuUnsupported) {
		t.Fatalf("missing session: %v", err)
	}
}

func TestGIOBindingFailure(t *testing.T) {
	for _, missingSymbol := range []bool{false, true} {
		var table gioBindings
		opened, closed := 0, 0
		err := bindGIO(&table, func(name string, flags int) (uintptr, error) {
			if !missingSymbol && opened == 1 {
				return 0, errors.New("library unavailable")
			}
			opened++
			return uintptr(opened), nil
		}, func(h uintptr, name string) (uintptr, error) { return 0, errors.New("symbol unavailable") }, func(h uintptr) error { closed++; return nil })
		if !errors.Is(err, types.ErrMenuUnsupported) {
			t.Fatalf("error: %v", err)
		}
		if closed != opened {
			t.Fatalf("handles opened %d closed %d", opened, closed)
		}
		if table.ContextNew != nil {
			t.Fatal("partial binding")
		}
	}
}
func TestGDBusFailureHelper(t *testing.T) {
	mode := os.Getenv("WEBVIEW_DBUS_FAIL_MODE")
	if mode == "" {
		return
	}
	requireGIO(t)
	if mode != "no-watcher" {
		mockWatcher(t)
	}
	_, err := NewTray(types.SystrayOptions{Icon: testIcon(t)})
	if !errors.Is(err, types.ErrMenuUnsupported) {
		t.Fatalf("expected unsupported for %s: %v", mode, err)
	}
	if err := trayBus.do(func() error {
		if len(trayBus.trays) != 0 {
			return errors.New("failure leaked tray")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := StopApplication(); err != nil {
		t.Fatal(err)
	}
}
func TestGDBusFailureCleanup(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("run with dbus-run-session")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"no-watcher", "no-host", "reject"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command("dbus-run-session", "--", exe, "-test.run=^TestGDBusFailureHelper$", "-test.timeout=20s")
			cmd.Env = append(os.Environ(), "WEBVIEW_DBUS_FAIL_MODE="+mode, "DISPLAY=", "WAYLAND_DISPLAY=")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

func TestDispatchStop(t *testing.T) {
	requireGIO(t)
	s := &dbusService{trays: map[uintptr]*linuxTray{}}
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.do(func() error { return nil })
			if err != nil && err != errBusStopped {
				t.Errorf("dispatch: %v", err)
			}
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.stop() }()
	}
	wg.Wait()
	if err := s.do(func() error { return nil }); err != errBusStopped {
		t.Fatal("dispatch accepted after stop")
	}
	empty := &dbusService{}
	empty.stop()
	if err := empty.start(); err != errBusStopped {
		t.Fatal("start accepted after stop")
	}
}
