package darwin

import (
	"errors"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	errNoSharedApplication = errors.New("darwin: no shared NSApplication")
	errNoWindow            = errors.New("darwin: failed to alloc NSWindow")
	errNoWebView           = errors.New("darwin: failed to alloc WKWebView")
)

type DialogKind int

const (
	DialogAlert DialogKind = iota
	DialogConfirm
	DialogPrompt
)

type SizeHint int

const (
	SizeNone SizeHint = iota
	SizeMin
	SizeMax
	SizeFixed
)

// NSApplicationActivationPolicyRegular = 0.
const activationRegular = 0

// NSWindow styleMask bits (NSWindowStyleMask).
const (
	styleTitled    = 1 << 0
	styleClosable  = 1 << 1
	styleResizable = 1 << 3
)

// NSBackingStoreBuffered = 2.
const backingBuffered = 2

// windowDelegateClass is registered once at package init; re-registering the
// same name panics.
var windowDelegateClass objc.Class

func init() {
	// AppKit and WebKit are not linked into a CGO_ENABLED=0 binary, so load them
	// explicitly before looking up their classes.
	for _, fw := range []string{
		"/System/Library/Frameworks/Cocoa.framework/Cocoa",
		"/System/Library/Frameworks/WebKit.framework/WebKit",
	} {
		if _, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
			panic(err)
		}
	}

	// windowShouldClose: returns whether the window should close when the user
	// clicks the close button. The window is the sender (one argument).
	windowShouldClose := func(id objc.ID, cmd objc.SEL, sender objc.ID) bool {
		return true
	}
	// applicationShouldTerminateAfterLastWindowClosed: keeps the app alive after
	// the last window closes so Run() only returns via Close()/terminate:.
	terminateAfterLastWindowClosed := func(id objc.ID, cmd objc.SEL, app objc.ID) bool {
		return false
	}
	var err error
	windowDelegateClass, err = objc.RegisterClass(
		"GoWebviewWindowDelegate",
		objc.GetClass("NSObject"),
		nil,
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("windowShouldClose:"), Fn: windowShouldClose},
			{Cmd: objc.RegisterName("applicationShouldTerminateAfterLastWindowClosed:"), Fn: terminateAfterLastWindowClosed},
		},
	)
	if err != nil {
		panic(err)
	}
}

type Platform struct {
	window   objc.ID
	webview  objc.ID
	app      objc.ID
	delegate objc.ID

	mu     sync.Mutex
	closed bool
	// runDone is closed by Close() to signal Run() to return.
	runDone chan struct{}
}

func New() *Platform {
	return &Platform{runDone: make(chan struct{})}
}

func (p *Platform) Run() error {
	runtime.LockOSThread()
	if err := p.setup(); err != nil {
		return err
	}
	p.app.Send(objc.RegisterName("run"))
	<-p.runDone
	return nil
}

func (p *Platform) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.runDone)
	p.mu.Unlock()

	if p.app != 0 {
		p.app.Send(objc.RegisterName("terminate:"), p.app)
	}
	return nil
}

// setup creates the NSApplication, NSWindow and WKWebView, then shows them.
func (p *Platform) setup() error {
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	if app == 0 {
		return errNoSharedApplication
	}
	p.app = app

	app.Send(objc.RegisterName("setActivationPolicy:"), activationRegular)
	app.Send(objc.RegisterName("activateIgnoringOtherApps:"), true)

	// Delegate: one per Platform, kept alive as a field.
	delegate := objc.ID(windowDelegateClass).Send(objc.RegisterName("alloc"))
	delegate = delegate.Send(objc.RegisterName("init"))
	p.delegate = delegate
	app.Send(objc.RegisterName("setDelegate:"), delegate)

	w := objc.ID(objc.GetClass("NSWindow")).Send(objc.RegisterName("alloc"))
	if w == 0 {
		return errNoWindow
	}
	styleMask := styleTitled | styleClosable | styleResizable
	w = w.Send(objc.RegisterName("initWithContentRect:styleMask:backing:defer:"),
		rect(0, 0, 800, 600), styleMask, backingBuffered, false)
	p.window = w
	w.Send(objc.RegisterName("setDelegate:"), delegate)

	config := objc.ID(objc.GetClass("WKWebViewConfiguration")).Send(objc.RegisterName("alloc"))
	config = config.Send(objc.RegisterName("init"))
	wv := objc.ID(objc.GetClass("WKWebView")).Send(objc.RegisterName("alloc"))
	if wv == 0 {
		return errNoWebView
	}
	wv = wv.Send(objc.RegisterName("initWithFrame:configuration:"), rect(0, 0, 800, 600), config)
	p.webview = wv

	w.Send(objc.RegisterName("setContentView:"), wv)
	w.Send(objc.RegisterName("center"))
	w.Send(objc.RegisterName("makeKeyAndOrderFront:"), 0)
	return nil
}

func (p *Platform) SetTitle(title string) error { return nil }
func (p *Platform) SetSize(width, height int, hint SizeHint) {
}
func (p *Platform) Navigate(url string) error { return nil }
func (p *Platform) SetHTML(html string) error { return nil }
func (p *Platform) Eval(js string) error      { return nil }
func (p *Platform) Dialog(kind DialogKind, message, defaultInput string) (string, bool) {
	return defaultInput, false
}
