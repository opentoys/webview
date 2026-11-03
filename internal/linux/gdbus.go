//go:build linux

package linux

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/opentoys/webview/internal/types"
)

// These bindings are deliberately independent of GTK/WebKit and their loader.
// All constructors below return floating variants; containers consume them.
// Values obtained with Child/GetVariant are owned and must be unreferenced.
type gioBindings struct {
	ContextNew       func() uintptr                                                                                            `sym:"g_main_context_new"`
	ContextPush      func(uintptr)                                                                                             `sym:"g_main_context_push_thread_default"`
	ContextPop       func(uintptr)                                                                                             `sym:"g_main_context_pop_thread_default"`
	ContextIteration func(uintptr, int32) int32                                                                                `sym:"g_main_context_iteration"`
	ContextUnref     func(uintptr)                                                                                             `sym:"g_main_context_unref"`
	SourceNew        func() uintptr                                                                                            `sym:"g_idle_source_new"`
	SourceCallback   func(uintptr, uintptr, uintptr, uintptr)                                                                  `sym:"g_source_set_callback"`
	SourceAttach     func(uintptr, uintptr) uint32                                                                             `sym:"g_source_attach"`
	SourceUnref      func(uintptr)                                                                                             `sym:"g_source_unref"`
	Free             func(uintptr)                                                                                             `sym:"g_free"`
	ErrorFree        func(uintptr)                                                                                             `sym:"g_error_free"`
	ObjectRef        func(uintptr) uintptr                                                                                     `sym:"g_object_ref"`
	ObjectUnref      func(uintptr)                                                                                             `sym:"g_object_unref"`
	Address          func(int32, uintptr, *uintptr) uintptr                                                                    `sym:"g_dbus_address_get_for_bus_sync"`
	Connect          func(string, uint32, uintptr, uintptr, *uintptr) uintptr                                                  `sym:"g_dbus_connection_new_for_address_sync"`
	ExitOnClose      func(uintptr, int32)                                                                                      `sym:"g_dbus_connection_set_exit_on_close"`
	Close            func(uintptr, uintptr, *uintptr) int32                                                                    `sym:"g_dbus_connection_close_sync"`
	Flush            func(uintptr, uintptr, *uintptr) int32                                                                    `sym:"g_dbus_connection_flush_sync"`
	Call             func(uintptr, string, string, string, string, uintptr, uintptr, uint32, int32, uintptr, *uintptr) uintptr `sym:"g_dbus_connection_call_sync"`
	Emit             func(uintptr, uintptr, string, string, string, uintptr, *uintptr) int32                                   `sym:"g_dbus_connection_emit_signal"`
	NodeNew          func(string, *uintptr) uintptr                                                                            `sym:"g_dbus_node_info_new_for_xml"`
	NodeLookup       func(uintptr, string) uintptr                                                                             `sym:"g_dbus_node_info_lookup_interface"`
	NodeUnref        func(uintptr)                                                                                             `sym:"g_dbus_node_info_unref"`
	Register         func(uintptr, string, uintptr, *dbusVTable, uintptr, uintptr, *uintptr) uint32                            `sym:"g_dbus_connection_register_object"`
	Unregister       func(uintptr, uint32) int32                                                                               `sym:"g_dbus_connection_unregister_object"`
	Return           func(uintptr, uintptr)                                                                                    `sym:"g_dbus_method_invocation_return_value"`
	ReturnError      func(uintptr, string, string)                                                                             `sym:"g_dbus_method_invocation_return_dbus_error"`
	NewString        func(string) uintptr                                                                                      `sym:"g_variant_new_string"`
	NewPath          func(string) uintptr                                                                                      `sym:"g_variant_new_object_path"`
	NewBool          func(int32) uintptr                                                                                       `sym:"g_variant_new_boolean"`
	NewInt           func(int32) uintptr                                                                                       `sym:"g_variant_new_int32"`
	NewUint          func(uint32) uintptr                                                                                      `sym:"g_variant_new_uint32"`
	NewByte          func(uint8) uintptr                                                                                       `sym:"g_variant_new_byte"`
	NewFixedArray    func(string, *byte, uintptr, uintptr) uintptr                                                             `sym:"g_variant_new_fixed_array"`
	NewVariant       func(uintptr) uintptr                                                                                     `sym:"g_variant_new_variant"`
	NewTuple         func(*uintptr, uintptr) uintptr                                                                           `sym:"g_variant_new_tuple"`
	NewArray         func(string, *uintptr, uintptr) uintptr                                                                   `sym:"g_variant_new_array"`
	NewEntry         func(uintptr, uintptr) uintptr                                                                            `sym:"g_variant_new_dict_entry"`
	Unref            func(uintptr)                                                                                             `sym:"g_variant_unref"`
	RefSink          func(uintptr) uintptr                                                                                     `sym:"g_variant_ref_sink"`
	Child            func(uintptr, uintptr) uintptr                                                                            `sym:"g_variant_get_child_value"`
	Count            func(uintptr) uintptr                                                                                     `sym:"g_variant_n_children"`
	String           func(uintptr, *uintptr) uintptr                                                                           `sym:"g_variant_get_string"`
	Int              func(uintptr) int32                                                                                       `sym:"g_variant_get_int32"`
	Uint             func(uintptr) uint32                                                                                      `sym:"g_variant_get_uint32"`
	Bool             func(uintptr) int32                                                                                       `sym:"g_variant_get_boolean"`
	Variant          func(uintptr) uintptr                                                                                     `sym:"g_variant_get_variant"`
	Signature        func(uintptr) uintptr                                                                                     `sym:"g_variant_get_type_string"`
}

var gio gioBindings

var errBusStopped = fmt.Errorf("%w: D-Bus service stopped", types.ErrMenuUnsupported)
var errTrayClosed = errors.New("webview: tray is closed")

var gioOnce sync.Once
var gioErr error

func loadGIO() error {
	gioOnce.Do(func() { gioErr = bindGIO(&gio, purego.Dlopen, purego.Dlsym, purego.Dlclose) })
	return gioErr
}

// The injectable loader also lets tests prove that library/symbol failures
// release opened handles and return ErrMenuUnsupported before binding.
func bindGIO(target *gioBindings, open func(string, int) (uintptr, error), lookup func(uintptr, string) (uintptr, error), closeLib func(uintptr) error) error {
	var handles []uintptr
	fail := func(err error) error {
		for _, h := range handles {
			_ = closeLib(h)
		}
		return fmt.Errorf("%w: %v", types.ErrMenuUnsupported, err)
	}
	for _, name := range []string{"libglib-2.0.so.0", "libgobject-2.0.so.0", "libgio-2.0.so.0"} {
		h, err := open(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			return fail(fmt.Errorf("%s: %v", name, err))
		}
		handles = append(handles, h)
	}
	// Resolve everything before registering anything: a missing symbol cannot
	// panic in RegisterLibFunc or leave a partially usable binding table.
	v := reflect.ValueOf(target).Elem()
	symbols := make([]uintptr, v.NumField())
	for i := range symbols {
		name := v.Type().Field(i).Tag.Get("sym")
		for _, h := range handles {
			p, err := lookup(h, name)
			if err == nil && p != 0 {
				symbols[i] = p
				break
			}
		}
		if symbols[i] == 0 {
			return fail(fmt.Errorf("missing symbol %s", name))
		}
	}
	for i, p := range symbols {
		purego.RegisterFunc(v.Field(i).Addr().Interface(), p)
	}
	// Keep successful library handles loaded for the bindings' lifetime.
	return nil
}

func nativeString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Pointer(p + uintptr(n))) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), n))
}
func takeGError(p uintptr) error {
	if p == 0 {
		return errors.New("GIO operation failed")
	}
	// GError: GQuark domain; gint code; gchar *message.
	e := (*struct {
		Domain  uint32
		Code    int32
		Message uintptr
	})(unsafe.Pointer(p))
	err := errors.New(nativeString(e.Message))
	gio.ErrorFree(p)
	return err
}
func vTuple(children ...uintptr) uintptr {
	return gio.NewTuple(unsafe.SliceData(children), uintptr(len(children)))
}
func vArray(element string, children ...uintptr) uintptr {
	return gio.NewArray(element, unsafe.SliceData(children), uintptr(len(children)))
}
func vBool(b bool) uintptr {
	if b {
		return gio.NewBool(1)
	}
	return gio.NewBool(0)
}
func vStrings(ss []string) uintptr {
	a := make([]uintptr, 0, len(ss))
	for _, s := range ss {
		a = append(a, gio.NewString(s))
	}
	return vArray("s", a...)
}
func vInts(ids []int32) uintptr {
	a := make([]uintptr, 0, len(ids))
	for _, id := range ids {
		a = append(a, gio.NewInt(id))
	}
	return vArray("i", a...)
}
func vReadString(v uintptr) string { return nativeString(gio.String(v, nil)) }
func vChildInt(v uintptr, i uintptr) int32 {
	c := gio.Child(v, i)
	defer gio.Unref(c)
	return gio.Int(c)
}
func vChildString(v uintptr, i uintptr) string {
	c := gio.Child(v, i)
	defer gio.Unref(c)
	return vReadString(c)
}
func vReadInts(v uintptr) []int32 {
	out := []int32{}
	for i := uintptr(0); i < gio.Count(v); i++ {
		out = append(out, vChildInt(v, i))
	}
	return out
}
func vReadStrings(v uintptr) []string {
	out := []string{}
	for i := uintptr(0); i < gio.Count(v); i++ {
		out = append(out, vChildString(v, i))
	}
	return out
}

type dbusVTable struct {
	Method, Get, Set uintptr
	Padding          [8]uintptr
}
type dbusTask struct {
	fn     func() error
	result chan error
}
type dbusService struct {
	mu             sync.Mutex
	ready, done    chan struct{}
	err            error
	context        uintptr
	stopped        bool
	quit           bool // only accessed on the context thread
	queue          []dbusTask
	sourcePending  bool
	sourceCallback uintptr
	trays          map[uintptr]*linuxTray // only accessed on the context thread
	nextID         uintptr
}

var trayBus = dbusService{trays: map[uintptr]*linuxTray{}}

func (s *dbusService) start() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return errBusStopped
	}
	if s.ready == nil {
		s.ready = make(chan struct{})
		s.done = make(chan struct{})
		go s.run()
	}
	ready := s.ready
	s.mu.Unlock()
	<-ready
	return s.err
}
func (s *dbusService) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.done)
	s.err = loadGIO()
	if s.err != nil {
		close(s.ready)
		return
	}
	s.context = gio.ContextNew()
	if s.context == 0 {
		s.err = fmt.Errorf("%w: GMainContext allocation", types.ErrMenuUnsupported)
		close(s.ready)
		return
	}
	gio.ContextPush(s.context)
	s.sourceCallback = purego.NewCallback(func(data uintptr) int32 {
		for {
			s.mu.Lock()
			tasks := s.queue
			s.queue = nil
			if len(tasks) == 0 {
				s.sourcePending = false
				s.mu.Unlock()
				return 0
			}
			s.mu.Unlock()
			for _, task := range tasks {
				task.result <- task.fn()
			}
		}
	})
	close(s.ready)
	for !s.quit {
		gio.ContextIteration(s.context, 1)
	}
	gio.ContextPop(s.context)
	gio.ContextUnref(s.context)
}
func (s *dbusService) enqueueLocked(fn func() error) <-chan error {
	ch := make(chan error, 1)
	s.queue = append(s.queue, dbusTask{fn, ch})
	if !s.sourcePending {
		s.sourcePending = true
		source := gio.SourceNew()
		gio.SourceCallback(source, s.sourceCallback, 0, 0)
		gio.SourceAttach(source, s.context)
		gio.SourceUnref(source)
	}
	return ch
}
func (s *dbusService) do(fn func() error) error {
	if err := s.start(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return errBusStopped
	}
	result := s.enqueueLocked(fn)
	s.mu.Unlock()
	return <-result
}
func (s *dbusService) stop() {
	s.mu.Lock()
	if s.stopped {
		done := s.done
		s.mu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	s.stopped = true
	ready := s.ready
	s.mu.Unlock()
	if ready == nil {
		return
	}
	<-ready
	if s.err != nil {
		<-s.done
		return
	}
	s.mu.Lock()
	result := s.enqueueLocked(func() error {
		for _, t := range s.trays {
			t.closeNative()
		}
		s.quit = true
		return nil
	})
	s.mu.Unlock()
	<-result
	<-s.done
}

// Outbound blocking calls must never run on the context thread: a watcher may
// query our exported objects before replying to RegisterStatusNotifierItem.
func dbusCall(conn uintptr, destination, path, iface, method string, args uintptr) (uintptr, error) {
	var e uintptr
	v := gio.Call(conn, destination, path, iface, method, args, 0, 0, 5000, 0, &e)
	if v == 0 {
		return 0, takeGError(e)
	}
	return v, nil
}
func connectSession() (uintptr, error) {
	var e uintptr
	address := gio.Address(2, 0, &e) // G_BUS_TYPE_SESSION
	if address == 0 {
		return 0, takeGError(e)
	}
	defer gio.Free(address)
	conn := gio.Connect(nativeString(address), 1|8, 0, 0, &e) // authentication client, message bus
	if conn == 0 {
		return 0, takeGError(e)
	}
	gio.ExitOnClose(conn, 0)
	return conn, nil
}
