package webview

import "github.com/opentoys/webview/internal/types"

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
	SaveFile(opts FileDialogOptions) (string, error)
}

// Options configures the webview. Field semantics are per-platform; each
// platform backend implements what it can.
type Options struct {
	Debug     bool
	Incognito bool
	DataDir   string
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
	// buildPlatform.
	w.p = buildPlatform(opts, w)
	return w, nil
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

// SaveFile shows a native save-file dialog and returns the chosen path, or ""
// when cancelled. Only implemented on Windows; other platforms return an error.
func (w *W) SaveFile(opts FileDialogOptions) (string, error) {
	return w.p.SaveFile(opts)
}
