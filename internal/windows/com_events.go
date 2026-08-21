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

// Vtable layout verified against WebView2 SDK WebView2.h.
type iCoreWebView2WebMessageReceivedEventArgsVtbl struct {
	_IUnknownVtbl
	GetSource               ComProc // 3: get_Source
	GetWebMessageAsJson     ComProc // 4: get_WebMessageAsJson
	TryGetWebMessageAsString ComProc // 5: TryGetWebMessageAsString
}

func (a *iCoreWebView2WebMessageReceivedEventArgs) GetWebMessageAsString() string {
	var pwstr *uint16
	a.vtbl.TryGetWebMessageAsString.Call(
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

// iCoreWebView2NavigationCompletedEventArgs wraps the event args COM interface.
type iCoreWebView2NavigationCompletedEventArgs struct {
	vtbl *iCoreWebView2NavigationCompletedEventArgsVtbl
}

type iCoreWebView2NavigationCompletedEventArgsVtbl struct {
	_IUnknownVtbl
	GetIsSuccess ComProc // 3: get_IsSuccess
}

func (a *iCoreWebView2NavigationCompletedEventArgs) GetIsSuccess() bool {
	var val uintptr
	a.vtbl.GetIsSuccess.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&val)),
	)
	return val != 0
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
