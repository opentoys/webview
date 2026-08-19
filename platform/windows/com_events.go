//go:build windows

package windows

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// iCoreWebView2WebMessageReceivedEventArgs wraps the event args COM interface.
type iCoreWebView2WebMessageReceivedEventArgs struct {
	vtbl *iCoreWebView2WebMessageReceivedEventArgsVtbl
}

type iCoreWebView2WebMessageReceivedEventArgsVtbl struct {
	_IUnknownVtbl
	GetSource               ComProc
	GetWebMessageAsString   ComProc
}

func (a *iCoreWebView2WebMessageReceivedEventArgs) GetWebMessageAsString() string {
	var pwstr *uint16
	a.vtbl.GetWebMessageAsString.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&pwstr)),
	)
	if pwstr == nil {
		return ""
	}
	s := windows.UTF16PtrToString(pwstr)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwstr)))
	return s
}

// iCoreWebView2PermissionRequestedEventHandler COM callback.
type iCoreWebView2PermissionRequestedEventHandler struct {
	vtbl *iCoreWebView2PermissionRequestedEventHandlerVtbl
	impl permissionRequestedImpl
}

type iCoreWebView2PermissionRequestedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type permissionRequestedImpl interface {
	InvokePermissionRequested(sender *iCoreWebView2, args uintptr) uintptr
}
