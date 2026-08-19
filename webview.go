package webview

// DialogKind, SizeHint and their constants are defined per-platform in
// platform_darwin.go / platform_other.go to avoid importing platform/darwin
// in the common file (which would break linux/windows cross-compile).

type Platform interface {
	Run() error
	Close() error
	SetTitle(title string) error
	SetSize(w, h int, hint SizeHint)
	Navigate(url string) error
	SetHTML(html string) error
	Eval(js string) error
	Dialog(kind DialogKind, message, defaultInput string) (string, bool)
}

// Options configures the webview. Field semantics are per-platform; each
// platform backend implements what it can.
type Options struct {
	// Debug enables web-developer tooling where the platform supports it.
	// Reserved for future backends; macOS currently has no effect (a WebKit
	// inspector depends on Safari's Develop service, unreachable from this
	// run loop).
	Debug bool
	// Incognito makes the webview use a non-persistent (in-memory) data store:
	// no cookies, cache, or localStorage written to disk.
	Incognito bool
	// DataDir sets the persistent website data store directory (cookies, cache,
	// localStorage). Empty = platform default. Ignored when Incognito is set.
	// macOS does not support a custom store directory (WKWebsiteDataStore has
	// no public initializer), so DataDir is ignored there.
	DataDir string
}

// OpenPanelParams describes the <input type=file> that triggered the picker.
type OpenPanelParams struct {
	AllowsMultipleSelection bool
	AllowsDirectories       bool
}

// OpenPanelFunc replaces the native file picker for <input type=file>. The
// handler must call callback with the absolute paths the user chose, or
// (nil,false) to cancel. callback is async and safe from any goroutine.
type OpenPanelFunc func(params OpenPanelParams, callback func(paths []string, ok bool))

type W struct {
	p         Platform
	bridge    *bridge
	dialog    func(kind DialogKind, message, defaultInput string) (string, bool)
	openPanel OpenPanelFunc
	// openPanelSet propagates the handler to the platform backend.
	// Set by buildPlatform; nil on backends that don't support file panels.
	openPanelSet func(OpenPanelFunc)
}

func New(opts Options) (*W, error) {
	w := &W{bridge: newBridge()}
	w.dialog = func(kind DialogKind, message, defaultInput string) (string, bool) {
		switch kind {
		case DialogConfirm:
			return "", false
		default:
			return defaultInput, true
		}
	}
	w.p = buildPlatform(opts, w)
	return w, nil
}

// Run blocks until the window closes or Close is called.
func (w *W) Run() error            { return w.p.Run() }
func (w *W) Close() error          { return w.p.Close() }
func (w *W) SetTitle(title string) { w.p.SetTitle(title) }
func (w *W) SetSize(width, height int, hint SizeHint) {
	w.p.SetSize(width, height, hint)
}
func (w *W) Navigate(url string) error { return w.p.Navigate(url) }
func (w *W) SetHTML(html string) error { return w.p.SetHTML(html) }
func (w *W) Eval(js string) error      { return w.p.Eval(js) }

// Bind registers Go func fn JS-callable. On the JS side call it via the bridge
// (Task 6 wires bootstrap + message routing).
func (w *W) Bind(name string, fn any) error {
	return w.bridge.Bind(name, fn)
}

// SetDialogHandler overrides the default JS dialog handler. The handler receives
// the dialog kind, the JS message/prompt text, and (for prompt) the default
// input. It returns (result, ok): for confirm, ok=true means "OK"; for prompt,
// ok=false means "cancel" and result is ignored.
func (w *W) SetDialogHandler(h func(kind DialogKind, message, defaultInput string) (string, bool)) {
	w.dialog = h
}

// SetOpenPanelHandler replaces the native file picker shown for
// <input type=file>. The handler must call callback with the absolute paths the
// user chose, or (nil,false) to cancel. callback is async and safe from any
// goroutine.
func (w *W) SetOpenPanelHandler(h OpenPanelFunc) {
	w.openPanel = h
	if w.openPanelSet != nil {
		w.openPanelSet(h)
	}
}
