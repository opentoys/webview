//go:build windows

package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DialogKind matches the webview package's DialogKind.
type DialogKind int

const (
	DialogAlert   DialogKind = iota
	DialogConfirm
	DialogPrompt
)

// SizeHint matches the webview package's SizeHint.
type SizeHint int

const (
	SizeNone  SizeHint = iota
	SizeMin
	SizeMax
	SizeFixed
)

// OpenPanelParams describes a file-open dialog trigger.
type OpenPanelParams struct {
	AllowsMultipleSelection bool
	AllowsDirectories       bool
}

// Platform implements the webview Platform interface for Windows/WebView2.
type Platform struct {
	// COM state (set during async init, read after ready==1).
	env        *iCoreWebView2Environment
	controller *iCoreWebView2Controller
	webview    *iCoreWebView2

	// Win32 state.
	hwnd       uintptr
	hwndWidget uintptr // child window hosting the WebView2 controller
	wndProc    uintptr

	// Callback wiring.
	MessageFunc   func(string)
	BoundFuncs    func() []string
	DialogFunc    func(kind DialogKind, message, defaultInput string) (string, bool)
	OpenPanelFunc func(params OpenPanelParams, callback func(paths []string, ok bool))
	DownloadFunc  func(suggestedFilename string, callback func(savePath string))

	// Options.
	Debug     bool
	Incognito bool
	DataDir   string

	// COM callback objects (prevent GC).
	envCompletedHandler  *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	ctrlCompletedHandler *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler
	msgReceivedHandler   *iCoreWebView2WebMessageReceivedEventHandler
	permRequestedHandler *iCoreWebView2PermissionRequestedEventHandler

	// Loader.
	createEnv WebView2CreateEnvironmentWithOptions

	// State.
	mu           sync.Mutex
	closed       bool
	pendingTitle string
	pendingHTML  string
	pendingURL   string
	runDone      chan struct{}
	ready        atomic.Int32
	dispatch     dispatchQueue
}

// New creates a Platform instance.
func New() *Platform {
	return &Platform{
		runDone:    make(chan struct{}),
		pendingURL: "about:blank",
	}
}

// Run blocks until the window is closed.
func (p *Platform) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := p.setup(); err != nil {
		return err
	}

	// Message loop.
	var msg MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 { // WM_QUIT
			break
		}
		if msg.Message == WM_APP {
			if fn := p.dispatch.pop(); fn != nil {
				fn()
			}
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	return nil
}

// setup creates the Win32 window and initializes WebView2.
func (p *Platform) setup() error {
	// COM initialization (STA).
	r, _, err := pCoInitializeEx.Call(0, COINIT_APARTMENTTHREADED)
	if r != S_OK && r != 1 { // S_FALSE = already initialized
		return fmt.Errorf("webview: CoInitializeEx failed: %w", err)
	}

	// Load WebView2Loader.
	createEnv, err := loadWebView2Loader()
	if err != nil {
		pCoUninitialize.Call()
		return err
	}
	p.createEnv = createEnv

	// Register window class.
	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := utf16PtrFromStr("GoWebviewWindow")
	p.wndProc = windows.NewCallback(p.wndproc)

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   p.wndProc,
		HInstance:     windows.Handle(hInst),
		LpszClassName: className,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// Create main window.
	title := utf16PtrFromStr(p.pendingTitle)
	p.hwnd, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		WS_OVERLAPPEDWINDOW,
		CW_USEDEFAULT, CW_USEDEFAULT, 800, 600,
		0, 0, hInst, 0,
	)
	runtime.KeepAlive(title)

	// Create child widget to host WebView2 controller.
	p.hwndWidget, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0,
		WS_CHILD|WS_VISIBLE,
		0, 0, 0, 0,
		p.hwnd, 0, hInst, 0,
	)
	runtime.KeepAlive(className)

	p.resizeWidget()
	pShowWindow.Call(p.hwnd, SW_SHOW)
	pUpdateWindow.Call(p.hwnd)

	// Start async WebView2 init.
	p.envCompletedHandler = newEnvCompletedHandler(p)
	p.ctrlCompletedHandler = newControllerCompletedHandler(p)
	p.msgReceivedHandler = newWebMessageReceivedHandler(p)
	p.permRequestedHandler = newPermissionRequestedHandler(p)

	// Default data dir: %AppData%\<exe-name>.
	dataDir := p.DataDir
	if dataDir == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.Getenv("LOCALAPPDATA")
		}
		if appData != "" {
			exe, _ := os.Executable()
			dataDir = filepath.Join(appData, filepath.Base(exe))
		}
	}
	if p.Incognito {
		dataDir = "" // empty = in-memory
	}

	pDataDir := utf16PtrFromStr(dataDir)
	pDataDirUPtr := uintptr(unsafe.Pointer(pDataDir))

	r = p.createEnv(0, pDataDirUPtr, 0, p.envCompletedHandler)
	runtime.KeepAlive(pDataDir)
	if r != S_OK {
		pCoUninitialize.Call()
		return fmt.Errorf("webview: CreateEnvironmentWithOptions failed: 0x%X", r)
	}

	return nil
}

// resizeWidget makes the child widget fill the main window's client area.
func (p *Platform) resizeWidget() {
	if p.hwndWidget == 0 || p.hwnd == 0 {
		return
	}
	var rc RECT
	pGetClientRect.Call(p.hwnd, uintptr(unsafe.Pointer(&rc)))
	pSetWindowPos.Call(
		p.hwndWidget, 0,
		0, 0,
		uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top),
		SWP_NOZORDER,
	)
	if p.controller != nil && p.ready.Load() != 0 {
		p.controller.PutBounds(RECT{
			Right:  rc.Right - rc.Left,
			Bottom: rc.Bottom - rc.Top,
		})
	}
}

// wndproc handles Win32 messages.
func (p *Platform) wndproc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_SIZE:
		p.resizeWidget()
		return 0
	case WM_APP:
		if fn := p.dispatch.pop(); fn != nil {
			fn()
		}
		return 0
	case WM_CLOSE:
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		if p.webview != nil {
			p.webview.Release()
			p.webview = nil
		}
		if p.controller != nil {
			p.controller.Release()
			p.controller = nil
		}
		if p.env != nil {
			p.env.Release()
			p.env = nil
		}
		pCoUninitialize.Call()
		pPostQuitMessage.Call(0)
		close(p.runDone)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// --- COM callback implementations ---

func (p *Platform) InvokeEnvCompleted(errorCode uintptr, env *iCoreWebView2Environment) uintptr {
	if errorCode != S_OK || env == nil {
		return 0
	}
	p.env = env
	env.CreateCoreWebView2Controller(p.hwndWidget, p.ctrlCompletedHandler)
	return 0
}

func (p *Platform) InvokeControllerCompleted(errorCode uintptr, controller *iCoreWebView2Controller) uintptr {
	if errorCode != S_OK || controller == nil {
		return 0
	}
	p.controller = controller
	controller.GetCoreWebView2(&p.webview)

	// Register message handler.
	var tok eventToken
	p.webview.AddWebMessageReceived(p.msgReceivedHandler, &tok)

	// Auto-grant permissions (file access, etc.).
	var permTok eventToken
	p.webview.AddPermissionRequested(p.permRequestedHandler, &permTok)

	// Inject bootstrap JS for all future pages.
	var names []string
	if p.BoundFuncs != nil {
		names = p.BoundFuncs()
	}
	js := bootstrapJS(names)
	p.webview.AddScriptToExecuteOnDocumentCreated(js, 0)

	// Configure settings.
	p.webview.PutAreDevToolsEnabled(p.Debug)
	p.webview.PutIsStatusBarEnabled(false)

	// Mark ready and apply pending state.
	p.ready.Store(1)

	// Set initial visibility and bounds (must be after ready.Store).
	controller.PutIsVisible(true)
	p.resizeWidget()

	p.mu.Lock()
	html := p.pendingHTML
	url := p.pendingURL
	p.mu.Unlock()

	if html != "" {
		p.webview.NavigateToString(html)
	} else if url != "" {
		p.webview.Navigate(url)
	}

	return 0
}

func (p *Platform) InvokeWebMessageReceived(sender *iCoreWebView2, args *iCoreWebView2WebMessageReceivedEventArgs) uintptr {
	msg := args.GetWebMessageAsString()
	if msg != "" && p.MessageFunc != nil {
		p.MessageFunc(msg)
	}
	return 0
}

func (p *Platform) InvokePermissionRequested(sender *iCoreWebView2, args uintptr) uintptr {
	return S_OK
}

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
	pPostMessageW.Call(p.hwnd, WM_CLOSE, 0, 0)
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
		SWP_NOZORDER|SWP_NOACTIVATE,
	)
}

func (p *Platform) Navigate(url string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready.Load() == 0 {
		p.pendingURL = url
		p.pendingHTML = ""
		return nil
	}
	p.webview.Navigate(url)
	return nil
}

func (p *Platform) SetHTML(html string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready.Load() == 0 {
		p.pendingHTML = html
		p.pendingURL = ""
		return nil
	}
	p.webview.NavigateToString(html)
	return nil
}

func (p *Platform) Eval(js string) error {
	if p.ready.Load() == 0 {
		return nil
	}
	p.webview.ExecuteScript(js, 0)
	return nil
}

// EvalHost evaluates JS from any goroutine by dispatching to the COM thread.
func (p *Platform) EvalHost(js string) {
	if p.ready.Load() == 0 {
		return
	}
	p.dispatch.push(func() {
		if p.webview != nil {
			p.webview.ExecuteScript(js, 0)
		}
	})
	pPostMessageW.Call(p.hwnd, WM_APP, 0, 0)
}

func (p *Platform) Dialog(kind DialogKind, message, defaultInput string) (string, bool) {
	if p.DialogFunc != nil {
		return p.DialogFunc(kind, message, defaultInput)
	}
	switch kind {
	case DialogConfirm:
		return "", false
	default:
		return defaultInput, true
	}
}
