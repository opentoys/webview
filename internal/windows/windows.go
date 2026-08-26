//go:build windows

package windows

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/opentoys/webview/internal/types"
)

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
	// COM state (set during async init, read after ready==1).
	env        *iCoreWebView2Environment
	controller *iCoreWebView2Controller
	webview    *iCoreWebView2

	// Win32 state.
	hwnd       uintptr
	hwndWidget uintptr // child window hosting the WebView2 controller
	wndProc    uintptr

	// Callback wiring.
	MessageFunc func(string)
	BoundFuncs  func() []string

	// Options.
	Debug     bool
	Incognito bool
	DataDir   string

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
func New() *Platform {
	return &Platform{
		runDone:        make(chan struct{}),
		pendingURL:     "about:blank",
		schemeHandlers: make(map[string]ResourceHandler),
		menuCallbacks:  make(map[uintptr]func()),
		nextMenuCmdID:  cmdCustomBase,
	}
}

// SetMenus replaces the native menu bar. Call before Run().
func (p *Platform) SetMenus(menus []Menu) {
	p.pendingMenus = menus
	p.hasCustomMenus = len(menus) > 0
}

// MainThread runs f on the Win32 UI thread, blocking until it completes.
// Reentrant: if already on the UI thread (e.g. a menu Action or bound func),
// f runs directly to avoid deadlocking the message loop.
func (p *Platform) MainThread(f func()) {
	if uiTID, _, _ := pGetWindowThreadProcessId.Call(p.hwnd, 0); uiTID != 0 {
		if curTID, _, _ := pGetCurrentThreadId.Call(); curTID == uiTID {
			f()
			return
		}
	}
	done := make(chan struct{})
	p.dispatch.push(func() {
		f()
		close(done)
	})
	pPostMessageW.Call(p.hwnd, WM_APP, 0, 0)
	<-done
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
	p.wndProc = syscall.NewCallback(p.wndproc)

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   p.wndProc,
		HInstance:     hInst,
		LpszClassName: className,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// Determine initial window size (use pending size if SetSize was called before Run).
	initW, initH := 800, 600
	if p.pendingW > 0 && p.pendingH > 0 {
		initW, initH = p.pendingW, p.pendingH
	}

	// Create main window.
	title := utf16PtrFromStr(p.pendingTitle)
	p.hwnd, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN,
		CW_USEDEFAULT, CW_USEDEFAULT, uintptr(initW), uintptr(initH),
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

	// Don't show the window yet — wait until WebView2 is initialized
	// and content is loaded (in InvokeControllerCompleted).
	p.resizeWidget()

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
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top
	pSetWindowPos.Call(
		p.hwndWidget, 0,
		0, 0,
		uintptr(w), uintptr(h),
		SWP_NOZORDER,
	)
	if p.controller != nil {
		p.controller.PutBounds(RECT{
			Right:  w,
			Bottom: h,
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
	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		// Custom menu callbacks.
		if cb, ok := p.menuCallbacks[cmdID]; ok {
			cb()
			return 0
		}
		// Built-in Edit menu.
		if p.webview != nil {
			var js string
			switch cmdID {
			case cmdUndo:
				js = "document.execCommand('undo')"
			case cmdRedo:
				js = "document.execCommand('redo')"
			case cmdCut:
				js = "document.execCommand('cut')"
			case cmdCopy:
				js = "document.execCommand('copy')"
			case cmdPaste:
				js = "document.execCommand('paste')"
			case cmdSelectAll:
				js = "document.execCommand('selectAll')"
			}
			if js != "" {
				p.webview.ExecuteScript(js, 0)
			}
		}
		return 0
	case WM_CLOSE:
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		// Only clean up for the main window, not the widget child window.
		if hwnd == p.hwnd {
			pPostQuitMessage.Call(0)
			pCoUninitialize.Call()
			close(p.runDone)
		}
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

	// AddRef to prevent COM objects from being released after callback returns.
	controller.vtbl.AddRef.Call(uintptr(unsafe.Pointer(controller)))
	p.controller = controller
	controller.GetCoreWebView2(&p.webview)
	p.webview.vtbl.AddRef.Call(uintptr(unsafe.Pointer(p.webview)))

	// Register PermissionRequested inside this callback (matches reference C++).
	var permTok eventToken
	p.webview.AddPermissionRequested(p.permRequestedHandler, &permTok)

	// Defer remaining heavy init to after this callback returns.
	p.dispatch.push(p.initWebView)
	pPostMessageW.Call(p.hwnd, WM_APP, 0, 0)

	return 0
}

// initWebView finishes WebView2 setup after the controller-completed callback
// has returned. It runs on the UI thread via the message loop.
func (p *Platform) initWebView() {
	// Register WebMessageReceived handler.
	var msgTok eventToken
	p.webview.AddWebMessageReceived(p.msgReceivedHandler, &msgTok)

	p.navCompletedHandler = newNavigationCompletedHandler(p)
	var navTok eventToken
	p.webview.AddNavigationCompleted(p.navCompletedHandler, &navTok)

	// Inject bootstrap JS for all future pages.
	var names []string
	if p.BoundFuncs != nil {
		names = p.BoundFuncs()
	}
	js := bootstrapJS(names)
	p.webview.AddScriptToExecuteOnDocumentCreated(js, 0)

	// Inject any user scripts accumulated before Run().
	for _, src := range p.userScriptSrcs {
		p.webview.AddScriptToExecuteOnDocumentCreated(src, 0)
	}
	p.userScriptSrcs = nil

	// Configure settings.
	p.webview.PutAreDevToolsEnabled(p.Debug)
	p.webview.PutIsStatusBarEnabled(false)

	// Register WebResourceRequested for intercepted schemes.
	if len(p.schemeHandlers) > 0 {
		p.webResourceHandler = newWebResourceRequestedHandler(p)
		p.webview.AddWebResourceRequested(p.webResourceHandler, &p.webResourceToken)
		for scheme := range p.schemeHandlers {
			// Filter: https://<scheme>.localhost/*
			filter := fmt.Sprintf("https://%s.localhost/*", scheme)
			p.webview.AddWebResourceRequestedFilter(filter, webResourceContextAll)
		}
	}

	// Intercept downloads: replace WebView2's native save dialog with ours.
	p.webview4 = p.webview.QueryInterface4()
	if p.webview4 != nil {
		p.downloadStartingHandler = newDownloadStartingHandler(p)
		p.webview4.AddDownloadStarting(p.downloadStartingHandler, &p.downloadStartingToken)
	}

	// Set bounds and visibility (reference order: resize → visible → show).
	p.resizeWidget()
	p.controller.PutIsVisible(true)

	// Show the widget child window explicitly.
	pShowWindow.Call(p.hwndWidget, SW_SHOW)
	pUpdateWindow.Call(p.hwndWidget)

	// Show the main window and focus.
	pShowWindow.Call(p.hwnd, SW_SHOW)
	pUpdateWindow.Call(p.hwnd)
	pSetForegroundWindow.Call(p.hwnd)
	pSetFocus.Call(p.hwnd)

	if p.hasCustomMenus {
		p.applyMenus(p.pendingMenus)
	} else {
		p.setupMainMenu()
	}

	// Mark ready so SetTitle/SetSize/SetHTML apply immediately.
	p.ready.Store(1)

	// Defer navigation to the next message-loop iteration.
	// AddScriptToExecuteOnDocumentCreated may be async internally;
	// giving the runtime one extra iteration ensures the script is
	// registered before NavigateToString creates the document.
	p.dispatch.push(p.navigatePending)
	pPostMessageW.Call(p.hwnd, WM_APP, 0, 0)
}

// navigatePending loads the initial URL or HTML after the bootstrap script
// has had time to register.
func (p *Platform) navigatePending() {
	p.mu.Lock()
	html := p.pendingHTML
	url := p.pendingURL
	p.mu.Unlock()

	if html != "" {
		p.webview.NavigateToString(html)
	} else if url != "" {
		p.webview.Navigate(p.rewriteSchemeURL(url))
	}
}

// setupMainMenu creates a Win32 menu bar with an Edit menu containing
// Undo, Redo, Cut, Copy, Paste, Select All.
func (p *Platform) setupMainMenu() {
	editMenu, _, _ := pCreatePopupMenu.Call()
	if editMenu == 0 {
		return
	}

	appendEditItem := func(label string, id uintptr) {
		putf16 := utf16PtrFromStr(label)
		pAppendMenuW.Call(editMenu, MF_STRING, id, uintptr(unsafe.Pointer(putf16)))
		runtime.KeepAlive(putf16)
	}

	appendEditItem("Undo\tCtrl+Z", cmdUndo)
	appendEditItem("Redo\tCtrl+Y", cmdRedo)
	pAppendMenuW.Call(editMenu, MF_SEPARATOR, 0, 0)
	appendEditItem("Cut\tCtrl+X", cmdCut)
	appendEditItem("Copy\tCtrl+C", cmdCopy)
	appendEditItem("Paste\tCtrl+V", cmdPaste)
	pAppendMenuW.Call(editMenu, MF_SEPARATOR, 0, 0)
	appendEditItem("Select All\tCtrl+A", cmdSelectAll)

	menuBar, _, _ := pCreateMenu.Call()
	if menuBar == 0 {
		return
	}

	editLabel := utf16PtrFromStr("Edit")
	pAppendMenuW.Call(menuBar, MF_POPUP, editMenu, uintptr(unsafe.Pointer(editLabel)))
	runtime.KeepAlive(editLabel)
	pSetMenu.Call(p.hwnd, menuBar)
}

// applyMenus builds and installs a Win32 menu bar from the given Menu slice.
func (p *Platform) applyMenus(menus []Menu) {
	p.menuCallbacks = make(map[uintptr]func())
	p.nextMenuCmdID = cmdCustomBase

	menuBar, _, _ := pCreateMenu.Call()
	if menuBar == 0 {
		return
	}

	for _, m := range menus {
		submenu, _, _ := pCreatePopupMenu.Call()
		if submenu == 0 {
			continue
		}
		for _, mi := range m.Items {
			if mi.Separator {
				pAppendMenuW.Call(submenu, MF_SEPARATOR, 0, 0)
				continue
			}
			label := mi.Label
			if mi.Shortcut != "" {
				label += "\t" + mi.Shortcut
			}
			cmdID := p.nextMenuCmdID
			p.nextMenuCmdID++
			if mi.Action != nil {
				p.menuCallbacks[cmdID] = mi.Action
			}
			putf16 := utf16PtrFromStr(label)
			pAppendMenuW.Call(submenu, MF_STRING, cmdID, uintptr(unsafe.Pointer(putf16)))
			runtime.KeepAlive(putf16)
		}
		topLabel := utf16PtrFromStr(m.Label)
		pAppendMenuW.Call(menuBar, MF_POPUP, submenu, uintptr(unsafe.Pointer(topLabel)))
		runtime.KeepAlive(topLabel)
	}

	pSetMenu.Call(p.hwnd, menuBar)
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

// InvokeDownloadStarting is called when WebView2 begins a download. It shows
// our IFileSaveDialog and writes the chosen path to ResultFilePath so WebView2
// downloads silently (no native save dialog). Cancelling the dialog cancels the
// download. Runs on the UI thread (COM callback).
func (p *Platform) InvokeDownloadStarting(sender *iCoreWebView2, args *iCoreWebView2DownloadStartingEventArgs) uintptr {
	// Hold a deferral: IFileSaveDialog::Show pumps its own message loop, and
	// without this WebView2 may finalize the download and free args while the
	// dialog is still open.
	deferral := args.GetDeferral()
	defer func() {
		if deferral != nil {
			deferral.Complete()
		}
	}()

	// Preferred: WebView2's default ResultFilePath already encodes the
	// suggested name (download attribute > Content-Disposition > URL).
	resultPath := args.GetResultFilePath()
	filename := basename(resultPath)
	dir := ""
	if resultPath != "" {
		dir = filepath.Dir(resultPath)
	}
	if filename == "" {
		if op := args.GetDownloadOperation(); op != nil {
			filename = filenameFromDisposition(op.GetContentDisposition())
			if filename == "" {
				filename = filenameFromURL(op.GetUri())
			}
			op.Release()
		}
	}

	path, err := p.saveFileDialog(FileDialogOptions{
		Title:     "Save File",
		Directory: dir,
		Filename:  filename,
	})
	if err != nil || path == "" {
		args.PutCancel(true)
		return S_OK
	}
	args.PutResultFilePath(path)
	args.PutHandled(true)
	return S_OK
}

// InvokeNavigationCompleted is called after each page navigation finishes.
// It injects the bootstrap JS so bound functions are available on the page.
func (p *Platform) InvokeNavigationCompleted(sender *iCoreWebView2, isSuccess bool) uintptr {
	// Inject the bootstrap JS after each navigation.
	var names []string
	if p.BoundFuncs != nil {
		names = p.BoundFuncs()
	}
	if len(names) == 0 {
		return 0
	}
	js := bootstrapJS(names)
	p.webview.ExecuteScript(js, 0)
	return 0
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
		return nil
	}
	p.webview.Navigate(p.rewriteSchemeURL(url))
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

// Init registers JS to run at document start for every page load.
func (p *Platform) Init(js string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.webview != nil {
		p.webview.AddScriptToExecuteOnDocumentCreated(js, 0)
	} else {
		p.userScriptSrcs = append(p.userScriptSrcs, js)
	}
	return nil
}

// rewriteSchemeURL converts scheme://host/path to https://scheme.localhost/path
// for registered schemes, so WebResourceRequested can intercept them.
// The https vhost gives a secure context (localStorage, crypto.subtle, etc.)
// without opening a TCP port.
func (p *Platform) rewriteSchemeURL(rawURL string) string {
	for scheme := range p.schemeHandlers {
		if strings.HasPrefix(rawURL, scheme+"://") {
			u, err := url.Parse(rawURL)
			if err != nil {
				return rawURL
			}
			out := fmt.Sprintf("https://%s.localhost/%s", scheme, strings.TrimPrefix(u.Path, "/"))
			if u.RawQuery != "" {
				out += "?" + u.RawQuery
			}
			if u.Fragment != "" {
				out += "#" + u.Fragment
			}
			return out
		}
	}
	return rawURL
}

// InvokeWebResourceRequested handles WebResourceRequested events from WebView2.
// Extracts the scheme from the URL, looks up the handler, and dispatches.
func (p *Platform) InvokeWebResourceRequested(sender *iCoreWebView2, args *iCoreWebView2WebResourceRequestedEventArgs) uintptr {
	req := args.GetRequest()
	if req == nil {
		return 0
	}

	uri := req.GetUri()
	if uri == "" {
		return 0
	}

	// Parse scheme from URL: https://app.localhost/path → "app"
	scheme := ""
	u, err := url.Parse(uri)
	if err == nil {
		host := u.Hostname()
		if idx := strings.Index(host, "."); idx > 0 {
			scheme = host[:idx]
		}
	}
	handler, ok := p.schemeHandlers[scheme]
	if !ok {
		return 0
	}

	method := req.GetMethod()
	if method == "" {
		method = http.MethodGet
	}
	headers := req.GetHeaders()

	sr := ResourceRequest{
		URL:     uri,
		Method:  method,
		Headers: headers,
	}
	switch method {
	case "GET", "HEAD", "TRACE", "OPTIONS":
	default:
		sr.Body = req.GetContent().ReadAll()
	}

	deferral := args.GetRequestDeferral()

	var gotResponse bool
	var syncResp *ResourceResponse
	handler(sr, func(resp *ResourceResponse) {
		if gotResponse {
			// Async response: dispatch to UI thread.
			p.dispatch.push(func() {
				p.applyResponse(args, deferral, resp)
			})
			pPostMessageW.Call(p.hwnd, WM_APP, 0, 0)
			return
		}
		// Synchronous response: store and apply after handler returns.
		gotResponse = true
		syncResp = resp
	})

	if gotResponse {
		p.applyResponse(args, deferral, syncResp)
	} else {
		deferral.Complete()
	}

	return 0
}

// applyResponse delivers the resource response to the webview via
// PutResponse + deferral. Called on the UI thread.
func (p *Platform) applyResponse(args *iCoreWebView2WebResourceRequestedEventArgs, deferral *iCoreWebView2Deferral, resp *ResourceResponse) {
	if resp == nil || len(resp.Body) == 0 {
		deferral.Complete()
		return
	}

	stream := createStreamOnHGlobal(resp.Body)
	if stream == nil {
		deferral.Complete()
		return
	}
	defer stream.vtbl.Release.Call(uintptr(unsafe.Pointer(stream)))

	webResp := p.env.CreateWebResourceResponse(
		"OK", resp.StatusCode, "", stream,
	)
	defer webResp.vtbl.Release.Call(uintptr(unsafe.Pointer(webResp)))

	if len(resp.Headers) > 0 {
		var parts []string
		for k, v := range resp.Headers {
			parts = append(parts, k+": "+strings.Join(v, ";"))
		}
		webResp.PutHeaders(strings.Join(parts, "\n"))
	}

	args.PutResponse(webResp)
	deferral.Complete()
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

// InterceptResource registers a resource handler for the given URL scheme.
// Must be called before Run(). scheme is the URL scheme without "://"
// (e.g. "app"). On Windows, URLs like app://path are rewritten to
// https://app.localhost/path (secure context) and intercepted via
// WebResourceRequested.
func (p *Platform) InterceptResource(scheme string, handler ResourceHandler) {
	p.schemeHandlers[scheme] = handler
}
