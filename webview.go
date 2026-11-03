package webview

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/opentoys/webview/internal/chrome"
	"github.com/opentoys/webview/internal/debuglog"
	"github.com/opentoys/webview/internal/types"
)

type SizeHint = types.SizeHint

const (
	SizeNone  = types.SizeNone
	SizeMin   = types.SizeMin
	SizeMax   = types.SizeMax
	SizeFixed = types.SizeFixed
)

// Backend selects the rendering backend. The values match the byte scheme
// 0x00–0x03 from the design; they are exposed as named string constants.
// Each backend's environment is probed in New: if the preferred backend is
// unavailable (returns nil/error), New falls back per the variant below.
const (
	// BackendWebview (0x00): the platform-native engine (WebKit/Edge).
	BackendWebview = ""
	// BackendChrome (0x01): Chrome/Chromium over the DevTools Protocol.
	BackendChrome = "chrome"
	// BackendFallbackWebview (0x02): Chrome when a Chrome/Chromium executable
	// is found, otherwise native. Fully resolvable at New.
	BackendFallbackWebview = "fallback-webview"
	// BackendFallbackChrome (0x03): native first; its libraries load in Run(),
	// so a missing native backend surfaces there, not at New — Chrome is the
	// documented runtime fallback only when native truly cannot initialize.
	BackendFallbackChrome = "fallback-chrome"
)

type ResourceRequest = types.ResourceRequest
type ResourceResponse = types.ResourceResponse
type ResourceHandler = types.ResourceHandler

type Menu = types.Menu
type MenuItem = types.MenuItem
type MenuItemRole = types.MenuItemRole

const (
	MenuItemRoleNone      = types.MenuItemRoleNone
	MenuItemRoleUndo      = types.MenuItemRoleUndo
	MenuItemRoleRedo      = types.MenuItemRoleRedo
	MenuItemRoleCut       = types.MenuItemRoleCut
	MenuItemRoleCopy      = types.MenuItemRoleCopy
	MenuItemRolePaste     = types.MenuItemRolePaste
	MenuItemRoleSelectAll = types.MenuItemRoleSelectAll
)

type FileFilter = types.FileFilter
type FileDialogOptions = types.FileDialogOptions

var (
	// ErrDialogCancelled is returned when a native chooser is dismissed.
	ErrDialogCancelled = types.ErrDialogCancelled
	// ErrDialogUnsupported is returned when native UI capabilities are unavailable.
	ErrDialogUnsupported = types.ErrDialogUnsupported
	// ErrDropUnsupported is returned when a backend cannot receive native file
	// drops. The Chrome/Chromium backend deliberately has no host window hook.
	ErrDropUnsupported = errors.New("webview: native file drops are unsupported by this backend")
)

// SizeHint, ResourceRequest, ResourceResponse, ResourceHandler are type aliases
// from types, re-exported per-platform in platform_*.go files.

type Platform interface {
	Run() error
	Close() error
	SetTitle(title string) error
	SetSize(w, h int, hint SizeHint)
	Navigate(url string) error
	SetHTML(html string) error
	Eval(js string) error
	Init(js string) error
	InterceptResource(scheme string, handler ResourceHandler)
	SetMenus(menus []Menu)
	MainThread(f func())
	// SetFilesDropped installs the native drop target. The callback must return
	// promptly; it only hands an immutable batch to W's dispatcher.
	SetFilesDropped(func([]string)) error
}

// Options configures the webview. Field semantics are per-platform; each
// platform backend implements what it can.
type Options struct {
	Debug     bool
	Incognito bool
	DataDir   string
	// Offscreen keeps the renderer active while its window is minimized or
	// occluded. It is false by default, preserving normal power saving.
	Offscreen bool
	// Backend selects the rendering backend. Empty/"" uses the platform
	// default (native). BackendChrome drives Chrome over the DevTools
	// Protocol; the Fallback* variants resolve at New time (see those
	// constants) by probing each backend's environment.
	Backend string
}

// W is defined per-platform:
//   - platform_darwin.go: includes dialog/openPanel/download handler fields
//   - platform_windows.go / platform_other.go: minimal struct
//
// W is the top-level webview handle. Darwin includes handler fields for
// dialog, file-panel, and download overrides.
type W struct {
	lifeMu                                sync.Mutex
	closed, started, hidden, closePending bool
	onClose                               func()
	closeDone                             chan struct{}
	menuMu                                sync.Mutex
	bindingMu                             sync.Mutex
	contextMenu                           types.ContextMenuConfig
	menuClosed                            bool

	p      Platform
	bridge *bridge

	dropMu     sync.Mutex
	dropFn     func([]string)
	dropClosed bool
	dropQueue  []dropEvent
	dropWake   chan struct{}
	dropDone   chan struct{}
	dropOnce   sync.Once
}

type dropEvent struct {
	fn    func([]string)
	paths []string
}

func New(opts Options) (*W, error) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.stopped {
		return nil, ErrApplicationStopped
	}
	w := &W{bridge: newBridge()}
	debugWriter := io.Discard
	if opts.Debug {
		debugWriter = os.Stdout
	}
	logger := debuglog.New(debugWriter)
	// Backend selection with environment probing. Each backend's feasibility
	// is checked here: if the preferred backend is unavailable (buildChrome
	// returns a nil platform or error), New falls back to the other per the
	// Backend* variant. Native backends load their libraries only in Run(), so
	// a missing native engine cannot be detected at New (it surfaces at Run) —
	// that is why BackendFallbackChrome resolves to native here.
	var err error
	switch opts.Backend {
	case BackendChrome, BackendFallbackWebview:
		w.p, err = buildChrome(opts, w, logger)
	case BackendWebview, BackendFallbackChrome: // BackendWebview ("")
		w.p, err = buildPlatform(opts, w, logger)
	}
	if err != nil {
		switch opts.Backend {
		case BackendFallbackWebview:
			w.p, err = buildPlatform(opts, w, logger)
		case BackendFallbackChrome:
			w.p, err = buildChrome(opts, w, logger)
		}
	}
	if err != nil {
		return nil, err
	}

	if w.p == nil {
		return nil, errors.New("webview: unknown backend")
	}

	// Install the platform's default menu bar (Edit on macOS/Linux; none on
	// others). Apps override via SetMenu. DefaultMenus is the single source of
	// truth, defined per platform.
	w.SetMenu(DefaultMenus(w)...)
	if app.running {
		if err := w.start(); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	app.registryMu.Lock()
	app.windows[w] = struct{}{}
	app.registryMu.Unlock()
	return w, nil
}

// buildChrome creates the Chrome/Chromium backend and wires the message handler
// to the shared bridge, mirroring buildPlatform for the native backends. It
// probes the Chrome environment: a missing executable is reported as an error
// so New() can fall back.
func buildChrome(opts Options, w *W, logger *debuglog.Logger) (Platform, error) {
	if chrome.ChromeExecutable() == "" {
		return nil, errors.New("webview: Chrome backend requested but no Chrome/Chromium executable found (set WEBVIEW_CHROME)")
	}
	p := chrome.New(chrome.Options{
		Debug:     opts.Debug,
		Incognito: opts.Incognito,
		DataDir:   opts.DataDir,
		Offscreen: opts.Offscreen,
	})
	p.Logger = logger
	p.BindingScript = w.bridge.bindingScript
	p.MessageFunc = func(body string) {
		w.dispatchMessage(body, p.EvalHost)
	}
	p.ClosedFunc = func() { _ = w.Close() }
	return p, nil
}

func (w *W) Run() error { return Run() }
func (w *W) Close() error {
	w.lifeMu.Lock()
	if w.closed {
		done := w.closeDone
		w.lifeMu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	w.closeDone = make(chan struct{})
	defer close(w.closeDone)
	w.closed = true
	if w.bridge != nil {
		w.bridge.Close()
	}
	defer func() { app.registryMu.Lock(); delete(app.windows, w); app.registryMu.Unlock() }()
	w.dropMu.Lock()
	w.dropClosed = true
	w.dropQueue = nil
	if w.dropDone != nil {
		close(w.dropDone)
	}
	w.dropFn = nil
	w.dropMu.Unlock()
	w.menuMu.Lock()
	w.menuClosed = true
	w.menuMu.Unlock()
	w.lifeMu.Unlock()
	return w.p.Close()
}
func (w *W) SetTitle(title string) error {
	return w.windowCall(func() error { return w.p.SetTitle(title) })
}
func (w *W) SetSize(width, height int, hint types.SizeHint) error {
	return w.windowCall(func() error { w.p.SetSize(width, height, hint); return nil })
}
func (w *W) Navigate(url string) error {
	return w.windowCall(func() error { return w.p.Navigate(url) })
}
func (w *W) SetHTML(html string) error {
	return w.windowCall(func() error { return w.p.SetHTML(html) })
}

// Eval submits JavaScript to the current document. Call after Run starts and
// the target page is ready; SetHTML and Navigate load asynchronously.
func (w *W) Eval(js string) error { return w.windowCall(func() error { return w.p.Eval(js) }) }

// Init registers JavaScript for the start of future documents. It is safe before
// Run and does not immediately execute in an already-loaded document.
func (w *W) Init(js string) error { return w.windowCall(func() error { return w.p.Init(js) }) }

// Bind exposes a function, a struct/non-nil *struct's exported methods and
// fields, or a non-nil map[string]any's functions and values. Properties are
// read-only JSON snapshots taken at Bind time; struct property names use Go
// field names. Names are literal window properties; rebinding replaces the entry.
func (w *W) Bind(name string, value any) error {
	return w.windowCall(func() error {
		w.bindingMu.Lock()
		defer w.bindingMu.Unlock()
		if err := w.bridge.Bind(name, value); err != nil {
			return err
		}
		return w.refreshBindings()
	})
}

// Unbind removes the whole named binding. Unknown names have no effect.
func (w *W) Unbind(name string) error {
	return w.windowCall(func() error {
		w.bindingMu.Lock()
		defer w.bindingMu.Unlock()
		if !w.bridge.Unbind(name) {
			return nil
		}
		return w.refreshBindings()
	})
}

func (w *W) InterceptResource(scheme string, handler ResourceHandler) error {
	return w.windowCall(func() error {
		if handler != nil {
			fn := handler
			handler = func(req ResourceRequest, respond func(*ResourceResponse)) {
				go func() {
					w.lifeMu.Lock()
					valid := !w.closed && !app.quitting.Load()
					w.lifeMu.Unlock()
					if !valid {
						respond(nil)
						return
					}
					defer func() {
						if recover() != nil {
							respond(nil)
						}
					}()
					fn(req, respond)
				}()
			}
		}
		w.p.InterceptResource(scheme, handler)
		return nil
	})
}

// SetMenu replaces the native menu bar. Each Menu becomes a top-level menu.
// Call before Run() for the initial bar, or after for live updates (not
// supported on all platforms — Linux requires re-layout).
func (w *W) SetMenu(menus ...Menu) error {
	return w.windowCall(func() error {
		snapshot := append([]Menu(nil), menus...)
		for i := range snapshot {
			snapshot[i].Items = w.menuCallbacks(snapshot[i].Items)
		}
		w.p.SetMenus(snapshot)
		return nil
	})
}

// MainThread runs f on the platform's UI thread, blocking until it completes.
// Use this when calling platform APIs that must be invoked from the main thread
// (e.g., native dialogs, window manipulation).
func (w *W) MainThread(f func()) error {
	w.lifeMu.Lock()
	closed, started := w.closed, w.started
	w.lifeMu.Unlock()
	if closed {
		return ErrClosed
	}
	if !started {
		return errors.New("webview: MainThread requires a running window")
	}
	var err error
	w.p.MainThread(func() {
		w.lifeMu.Lock()
		closed := w.closed
		w.lifeMu.Unlock()
		if closed {
			err = ErrClosed
			return
		}
		f()
	})
	return err
}

// OnFilesDropped receives local files and directories dropped on the native
// window content area. While installed it consumes these drops before the web
// page sees them. fn is invoked asynchronously, in drop order, and never on a
// platform UI thread. Passing nil removes the handler and restores the
// browser's normal drag-and-drop behaviour.
func (w *W) OnFilesDropped(fn func(paths []string)) error {
	return w.windowCall(func() error { return w.onFilesDropped(fn) })
}
func (w *W) onFilesDropped(fn func([]string)) error {
	w.dropMu.Lock()
	defer w.dropMu.Unlock()
	if w.dropClosed {
		return ErrClosed
	}
	if err := w.p.SetFilesDropped(w.captureFilesDropped(fn)); err != nil {
		if errors.Is(err, chrome.ErrDropUnsupported) {
			return ErrDropUnsupported
		}
		return err
	}
	w.dropFn = fn
	return nil
}

// captureFilesDropped returns nil when drops are disabled, allowing platforms
// to restore their normal browser behaviour without knowing W's state.
func (w *W) captureFilesDropped(fn func([]string)) func([]string) {
	if fn == nil {
		return nil
	}
	return w.filesDropped
}

// filesDropped is called by a platform UI implementation. It makes all data
// independent of native object lifetimes and snapshots the handler at capture
// time, then hands it to a single worker.
func (w *W) filesDropped(paths []string) {
	clean := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		clean = append(clean, filepath.Clean(path))
	}
	if len(clean) == 0 {
		return
	}
	w.dropMu.Lock()
	if w.dropClosed || w.dropFn == nil {
		w.dropMu.Unlock()
		return
	}
	e := dropEvent{fn: w.dropFn, paths: clean}
	w.dropOnce.Do(func() {
		w.dropWake = make(chan struct{}, 1)
		w.dropDone = make(chan struct{})
		go w.runDropQueue()
	})
	w.dropQueue = append(w.dropQueue, e)
	wake := w.dropWake
	w.dropMu.Unlock()
	// Coalesce wakeups. The queue itself is protected by dropMu and is never
	// bounded, so this cannot hold up a native UI callback.
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (w *W) runDropQueue() {
	for {
		select {
		case <-w.dropDone:
			return
		case <-w.dropWake:
		}
		for {
			w.dropMu.Lock()
			if len(w.dropQueue) == 0 {
				w.dropMu.Unlock()
				break
			}
			e := w.dropQueue[0]
			w.dropQueue[0] = dropEvent{}
			w.dropQueue = w.dropQueue[1:]
			closed := w.dropClosed || app.quitting.Load()
			w.dropMu.Unlock()
			if !closed {
				e.fn(append([]string(nil), e.paths...))
			}
		}
	}
}

func (w *W) menuCallbacks(items []MenuItem) []MenuItem {
	result := append([]MenuItem(nil), items...)
	for i := range result {
		if fn := result[i].Action; fn != nil {
			result[i].Action = func() {
				w.menuMu.Lock()
				valid := !w.menuClosed && !app.quitting.Load()
				w.menuMu.Unlock()
				if valid {
					fn()
				}
			}
		}
	}
	return result
}

func (w *W) refreshBindings() error {
	if p, ok := w.p.(interface{ RefreshBindings() error }); ok {
		return p.RefreshBindings()
	}
	return nil
}
