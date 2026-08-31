package webview

import (
	"errors"
	"io"
	"os"

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

type FileFilter = types.FileFilter
type FileDialogOptions = types.FileDialogOptions

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
}

// Options configures the webview. Field semantics are per-platform; each
// platform backend implements what it can.
type Options struct {
	Debug     bool
	Incognito bool
	DataDir   string
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
	p      Platform
	bridge *bridge
}

func New(opts Options) (*W, error) {
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

	// Install the platform's default menu bar (Edit on macOS/Linux; none on
	// others). Apps override via SetMenu. DefaultMenus is the single source of
	// truth, defined per platform.
	w.SetMenu(DefaultMenus(w)...)
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
	})
	p.Logger = logger
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	return p, nil
}

func (w *W) Run() error            { return w.p.Run() }
func (w *W) Close() error          { return w.p.Close() }
func (w *W) SetTitle(title string) { w.p.SetTitle(title) }
func (w *W) SetSize(width, height int, hint types.SizeHint) {
	w.p.SetSize(width, height, hint)
}
func (w *W) Navigate(url string) error { return w.p.Navigate(url) }
func (w *W) SetHTML(html string) error { return w.p.SetHTML(html) }
func (w *W) Eval(js string) error      { return w.p.Eval(js) }
func (w *W) Init(js string) error      { return w.p.Init(js) }

func (w *W) Bind(name string, fn any) error {
	return w.bridge.Bind(name, fn)
}

func (w *W) Unbind(name string) {
	w.bridge.Unbind(name)
}

func (w *W) InterceptResource(scheme string, handler ResourceHandler) {
	w.p.InterceptResource(scheme, handler)
}

// SetMenu replaces the native menu bar. Each Menu becomes a top-level menu.
// Call before Run() for the initial bar, or after for live updates (not
// supported on all platforms — Linux requires re-layout).
func (w *W) SetMenu(menus ...Menu) {
	w.p.SetMenus(menus)
}

// MainThread runs f on the platform's UI thread, blocking until it completes.
// Use this when calling platform APIs that must be invoked from the main thread
// (e.g., native dialogs, window manipulation).
func (w *W) MainThread(f func()) {
	w.p.MainThread(f)
}
