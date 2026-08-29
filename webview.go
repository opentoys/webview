package webview

import (
	"github.com/opentoys/webview/internal/chrome"
	"github.com/opentoys/webview/internal/types"
)

type SizeHint = types.SizeHint

const (
	SizeNone  = types.SizeNone
	SizeMin   = types.SizeMin
	SizeMax   = types.SizeMax
	SizeFixed = types.SizeFixed
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
	// Backend selects the rendering backend. Empty uses the platform default
	// (native). "chrome" drives Chrome/Chromium over the DevTools Protocol.
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
	// Platform-specific initialization (e.g. dialog handler) happens in
	// buildPlatform (or buildChrome for the Chrome backend).
	if opts.Backend == "chrome" {
		w.p = buildChrome(opts, w)
	} else {
		w.p = buildPlatform(opts, w)
	}
	// Install the platform's default menu bar (Edit on macOS/Linux; none on
	// others). Apps override via SetMenu. DefaultMenus is the single source of
	// truth, defined per platform.
	w.SetMenu(DefaultMenus(w)...)
	return w, nil
}

// buildChrome creates the Chrome/Chromium backend and wires the message handler
// to the shared bridge, mirroring buildPlatform for the native backends.
func buildChrome(opts Options, w *W) Platform {
	p := chrome.New(chrome.Options{
		Debug:     opts.Debug,
		Incognito: opts.Incognito,
		DataDir:   opts.DataDir,
	})
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	return p
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
