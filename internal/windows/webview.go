//go:build windows

package windows

// WebView2 initialization and event callbacks.

import (
	"fmt"
	"path/filepath"
	"unsafe"
)

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
