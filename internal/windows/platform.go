//go:build windows

package windows

// Platform state, lifecycle, and public window/webview operations.

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/opentoys/webview/internal/debuglog"
	"github.com/opentoys/webview/internal/types"
)

const BackendName string = "webview"

// Re-export shared types from types.
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
type FileDialogOptions = types.FileDialogOptions
type FileFilter = types.FileFilter

// Platform implements the webview Platform interface for Windows/WebView2.
type Platform struct {
	closeRequest                func()
	hidden                      bool
	initDone                    chan error
	trayOptions                 *types.SystrayOptions
	trayIcon                    uintptr
	trayAdded                   bool
	trayItems                   []types.MenuItem
	trayActions, contextActions types.MenuActions
	taskbarCreated              uintptr
	contextOptions              types.ContextMenuConfig
	contextConfigured           bool
	contextView, contextEnv     uintptr
	contextToken                eventToken
	contextHandler              *nativeMenuEvent
	contextItems                []nativeContextItem
	menuInitErr                 error

	// COM state (set during async init, read after ready==1).
	env        *iCoreWebView2Environment
	controller *iCoreWebView2Controller
	webview    *iCoreWebView2

	// Win32 state.
	hwnd       uintptr
	hwndWidget uintptr // child window hosting the WebView2 controller
	wndProc    uintptr

	// Callback wiring.
	MessageFunc             func(string)
	documentScriptIDs       []string
	bindingScriptGeneration uint64
	bindingScriptHandlers   map[*scriptCompletedHandler]struct{}
	BindingScript           func() string
	BoundFuncs              func() []string

	// Options.
	Debug     bool
	Logger    *debuglog.Logger
	Incognito bool
	DataDir   string
	// Offscreen keeps the WebView2 controller active while the parent window is
	// minimized or occluded.
	Offscreen       bool
	offscreenActive bool
	filesDropped    func([]string)

	// COM callback objects (prevent GC).
	envCompletedHandler     *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	ctrlCompletedHandler    *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler
	msgReceivedHandler      *iCoreWebView2WebMessageReceivedEventHandler
	permRequestedHandler    *iCoreWebView2PermissionRequestedEventHandler
	navCompletedHandler     *iCoreWebView2NavigationCompletedEventHandler
	webResourceHandler      *iCoreWebView2WebResourceRequestedEventHandler
	webResourceToken        eventToken
	webview4                *iCoreWebView2_4
	downloadStartingHandler *iCoreWebView2DownloadStartingEventHandler
	downloadStartingToken   eventToken

	// Resource interception.
	schemeHandlers map[string]ResourceHandler
	// userScriptSrcs accumulates JS sources added via Init() before the
	// webview is ready. They are injected via AddScriptToExecuteOnDocumentCreated.
	userScriptSrcs []string

	pendingMenus   []Menu
	hasCustomMenus bool
	// menuCallbacks maps command IDs to Go callbacks for custom menus.
	menuCallbacks map[uintptr]func()
	nextMenuCmdID uintptr

	// Loader.
	createEnv WebView2CreateEnvironmentWithOptions

	// State.
	mu           sync.Mutex
	closed       bool
	pendingTitle string
	pendingHTML  string
	pendingURL   string
	pendingW     int
	pendingH     int
	runDone      chan struct{}
	ready        atomic.Int32
	dispatch     dispatchQueue
}

// Edit menu command IDs.
const (
	cmdUndo = 1001 + iota
	cmdRedo
	cmdCut
	cmdCopy
	cmdPaste
	cmdSelectAll

	cmdCustomBase = 2000
)

// New creates a Platform instance.
func New() (*Platform, error) {

	return &Platform{
		runDone:        make(chan struct{}),
		initDone:       make(chan error, 1),
		pendingURL:     "about:blank",
		schemeHandlers: make(map[string]ResourceHandler),
		menuCallbacks:  make(map[uintptr]func()),
		nextMenuCmdID:  cmdCustomBase,
	}, nil
}

// SetMenus replaces the native menu bar. Call before Run().
func (p *Platform) SetMenus(menus []Menu) {
	p.pendingMenus = menus
	p.hasCustomMenus = len(menus) > 0
}

// MainThread runs f on the Win32 UI thread, blocking until it completes.
// Reentrant: if already on the UI thread (e.g. a menu Action or bound func),
// f runs directly to avoid deadlocking the message loop.
func (p *Platform) MainThread(f func()) { applicationThread(f) }
func (p *Platform) Run() error          { return p.Start() }

// setup creates the Win32 window and initializes WebView2.
// --- Platform interface methods ---

// Close destroys the window, causing Run() to return.
func (p *Platform) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	select {
	case <-applicationReady:
		if applicationErr == nil {
			applicationThread(func() {
				if p.hwnd != 0 {
					pDestroyWindow.Call(p.hwnd)
				}
			})
		}
	default:
	}
	return nil
}

// SetFilesDropped configures the host drop target. It is retained before
// WebView2 initializes and installed by initWebView once the widget exists.
func (p *Platform) SetFilesDropped(fn func([]string)) error {
	p.mu.Lock()
	p.filesDropped = fn
	p.mu.Unlock()
	if p.ready.Load() != 0 {
		return p.setDropTarget(fn)
	}
	return nil
}

func (p *Platform) SetTitle(title string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready.Load() == 0 {
		p.pendingTitle = title
		return nil
	}
	pTitle := utf16PtrFromStr(title)
	pSetWindowTextW.Call(p.hwnd, uintptr(unsafe.Pointer(pTitle)))
	runtime.KeepAlive(pTitle)
	return nil
}

func (p *Platform) SetSize(w, h int, hint SizeHint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready.Load() == 0 {
		p.pendingW = w
		p.pendingH = h
		return
	}
	var rc RECT
	rc.Right = int32(w)
	rc.Bottom = int32(h)
	pAdjustWindowRectEx.Call(
		uintptr(unsafe.Pointer(&rc)),
		WS_OVERLAPPEDWINDOW, 0, 0,
	)
	pSetWindowPos.Call(
		p.hwnd, 0,
		0, 0,
		uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top),
		SWP_NOZORDER|SWP_NOMOVE|SWP_NOACTIVATE,
	)
}

func (p *Platform) Navigate(url string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready.Load() == 0 {
		p.pendingURL = url
		p.pendingHTML = ""
		p.Logger.Log(BackendName, "navigate", map[string]string{"url": debuglog.URL(url), "phase": "queued"})
		return nil
	}
	p.webview.Navigate(p.rewriteSchemeURL(url))
	p.Logger.Log(BackendName, "navigate", map[string]string{"url": debuglog.URL(url), "phase": "started"})
	return nil
}

func (p *Platform) SetHTML(html string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready.Load() == 0 {
		p.pendingHTML = html
		p.pendingURL = ""
		p.Logger.Log(BackendName, "load_html", map[string]string{"bytes": strconv.Itoa(len(html)), "phase": "queued"})
		return nil
	}
	p.webview.NavigateToString(html)
	p.Logger.Log(BackendName, "load_html", map[string]string{"bytes": strconv.Itoa(len(html)), "phase": "started"})
	return nil
}

func (p *Platform) Eval(js string) error {
	if p.ready.Load() == 0 {
		return errors.New("webview: Eval requires a running window; use Init before Run")
	}
	var err error
	p.MainThread(func() {
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed {
			err = errors.New("webview: Eval after Close")
			return
		}
		if hr := p.webview.ExecuteScript(js, 0); hr != S_OK {
			err = fmt.Errorf("webview: ExecuteScript failed: 0x%X", hr)
		}
	})
	return err
}

// Init registers JS to run at document start for every page load.
func (p *Platform) Init(js string) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("webview: Init after Close")
	}
	p.userScriptSrcs = append(p.userScriptSrcs, js)
	ready := p.webview != nil
	p.mu.Unlock()
	if !ready {
		return nil
	}
	var err error
	p.MainThread(func() { err = p.rebuildBindingScript() })
	return err
}

// rewriteSchemeURL converts scheme://host/path to https://scheme.localhost/path
// for registered schemes, so WebResourceRequested can intercept them.
// The https vhost gives a secure context (localStorage, crypto.subtle, etc.)
// without opening a TCP port.
