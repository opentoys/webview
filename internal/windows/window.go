//go:build windows

package windows

// Win32 window creation, sizing, and message handling.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"
)

func (p *Platform) setup() error {
	if p.Offscreen {
		// WebView2's controller already remains visible while the parent window
		// is minimized. Keep this UI thread eligible to run so its renderer does
		// not get suspended by Windows' background power policy.
		pSetThreadExecutionState.Call(ES_CONTINUOUS | ES_SYSTEM_REQUIRED)
		p.offscreenActive = true
	}
	// Load WebView2Loader.
	createEnv, err := loadWebView2Loader()
	if err != nil {
		return err
	}
	p.createEnv = createEnv

	// Register window class.
	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := registerNativeClass()

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
	if p.hwnd == 0 {
		return fmt.Errorf("webview: failed to create native window")
	}
	nativeWindowOwners[p.hwnd] = p

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
	if p.hwndWidget == 0 {
		return fmt.Errorf("webview: failed to create WebView host")
	}
	nativeWindowOwners[p.hwndWidget] = p

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

	r := p.createEnv(0, pDataDirUPtr, 0, p.envCompletedHandler)
	runtime.KeepAlive(pDataDir)
	if r != S_OK {
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
	if p.taskbarCreated != 0 && msg == p.taskbarCreated && hwnd == p.hwnd {
		p.trayAdded = false
		_ = p.applySystray()
		return 0
	}
	switch msg {
	case trayMessage:
		if lParam == 0x205 || lParam == 0x203 {
			p.showTrayMenu()
		}
		return 0

	case WM_SIZE:
		p.resizeWidget()
		if wParam == SIZE_MINIMIZED && p.Offscreen && p.controller != nil {
			// Do not let a minimized parent mark the WebView2 controller hidden:
			// IsVisible=false suppresses rendering and event delivery.
			p.controller.PutIsVisible(true)
		}
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
			go cb()
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
		if hwnd == p.hwnd && p.closeRequest != nil {
			p.closeRequest()
			return 0
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		// Only clean up for the main window, not the widget child window.
		if hwnd == p.hwnd {
			p.mu.Lock()
			p.closed = true
			p.mu.Unlock()
			p.removeSystray()
			p.closeContextMenu()
			if p.webview4 != nil {
				p.webview4.vtbl.Release.Call(uintptr(unsafe.Pointer(p.webview4)))
				p.webview4 = nil
			}

			if p.offscreenActive {
				pSetThreadExecutionState.Call(ES_CONTINUOUS)
				p.offscreenActive = false
			}
			p.Logger.Log(BackendName, "closed", nil)
			if p.controller != nil {
				p.controller.Close()
				p.controller.vtbl.Release.Call(uintptr(unsafe.Pointer(p.controller)))
				p.controller = nil
			}
			if p.webview != nil {
				p.webview.vtbl.Release.Call(uintptr(unsafe.Pointer(p.webview)))
				p.webview = nil
			}
			if p.env != nil {
				p.env.vtbl.Release.Call(uintptr(unsafe.Pointer(p.env)))
				p.env = nil
			}
			p.hwnd = 0

			close(p.runDone)
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}
