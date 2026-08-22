package webview

// SizeHint, ResourceRequest, ResourceResponse, ResourceHandler are type aliases
// from internal/types, re-exported per-platform in platform_*.go files.

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
func (w *W) SetSize(width, height int, hint SizeHint) {
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
