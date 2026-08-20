//go:build windows

package windows

import (
	"sync"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")

	pRegisterClassExW   = user32.NewProc("RegisterClassExW")
	pCreateWindowExW    = user32.NewProc("CreateWindowExW")
	pDefWindowProcW     = user32.NewProc("DefWindowProcW")
	pGetMessageW        = user32.NewProc("GetMessageW")
	pTranslateMessage   = user32.NewProc("TranslateMessage")
	pDispatchMessageW   = user32.NewProc("DispatchMessageW")
	pShowWindow         = user32.NewProc("ShowWindow")
	pUpdateWindow       = user32.NewProc("UpdateWindow")
	pSetWindowTextW     = user32.NewProc("SetWindowTextW")
	pSetWindowPos       = user32.NewProc("SetWindowPos")
	pAdjustWindowRectEx = user32.NewProc("AdjustWindowRectEx")
	pDestroyWindow      = user32.NewProc("DestroyWindow")
	pPostQuitMessage    = user32.NewProc("PostQuitMessage")
	pGetClientRect      = user32.NewProc("GetClientRect")
	pSetFocus           = user32.NewProc("SetFocus")
	pGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	pPostMessageW       = user32.NewProc("PostMessageW")
	pCoInitializeEx     = ole32.NewProc("CoInitializeEx")
	pCoUninitialize     = ole32.NewProc("CoUninitialize")
	pCoTaskMemFree      = ole32.NewProc("CoTaskMemFree")
)

// Window styles.
const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_CLIPCHILDREN     = 0x02000000
	CW_USEDEFAULT       = 0x80000000
)

// ShowWindow commands.
const SW_SHOW = 5

// Window messages.
const (
	WM_SIZE    = 0x0005
	WM_CLOSE   = 0x0010
	WM_DESTROY = 0x0002
	WM_APP     = 0x8000
)

// SetWindowPos flags.
const (
	SWP_NOMOVE     = 0x0002
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010
)

// WNDCLASSEXW matches the Win32 WNDCLASSEXW structure.
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

// RECT matches the Win32 RECT structure.
type RECT struct {
	Left, Top, Right, Bottom int32
}

// POINT matches the Win32 POINT structure.
type POINT struct {
	X, Y int32
}

// MSG matches the Win32 MSG structure.
type MSG struct {
	HWnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// dispatchQueue is a thread-safe queue of functions to run on the host thread.
// Used for cross-goroutine dispatch via PostMessage(WM_APP).
type dispatchQueue struct {
	mu    sync.Mutex
	funcs []func()
}

func (q *dispatchQueue) push(fn func()) {
	q.mu.Lock()
	q.funcs = append(q.funcs, fn)
	q.mu.Unlock()
}

func (q *dispatchQueue) pop() func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.funcs) == 0 {
		return nil
	}
	fn := q.funcs[0]
	q.funcs = q.funcs[1:]
	return fn
}

// eventToken is used by WebView2 event subscription APIs.
type eventToken int64
