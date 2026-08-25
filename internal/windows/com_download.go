//go:build windows

package windows

import (
	"runtime"
	"strings"
	"unsafe"
)

// Download interception via the DownloadStarting event, exposed on
// ICoreWebView2_4. When a download begins we show our own IFileSaveDialog and
// put its path into ResultFilePath; WebView2 then downloads silently to that
// path without showing its native save dialog.

// IID_ICoreWebView2_4.
var iidCoreWebView2_4 = GUID{0x20D02D59, 0x6DF2, 0x42DC, [8]byte{0xBD, 0x06, 0xF9, 0x8A, 0x69, 0x4B, 0x13, 0x02}}

// --- ICoreWebView2_4 -------------------------------------------------------

// iCoreWebView2_4Vtbl extends the full base ICoreWebView2 vtable through
// ICoreWebView2_2 and ICoreWebView2_3, so add_DownloadStarting lands at slot
// 75 (0-indexed, IUnknown = 0..2).
type iCoreWebView2_4Vtbl struct {
	iCoreWebView2Vtbl
	// ICoreWebView2_2.
	AddWebResourceResponseReceived    ComProc // 61
	RemoveWebResourceResponseReceived ComProc // 62
	NavigateWithWebResourceRequest    ComProc // 63
	AddDOMContentLoaded               ComProc // 64
	RemoveDOMContentLoaded            ComProc // 65
	GetCookieManager                  ComProc // 66
	GetEnvironment                    ComProc // 67
	// ICoreWebView2_3.
	TrySuspend                          ComProc // 68
	Resume                              ComProc // 69
	GetIsSuspended                      ComProc // 70
	SetVirtualHostNameToFolderMapping   ComProc // 71
	ClearVirtualHostNameToFolderMapping ComProc // 72
	// ICoreWebView2_4.
	AddFrameCreated        ComProc // 73
	RemoveFrameCreated     ComProc // 74
	AddDownloadStarting    ComProc // 75
	RemoveDownloadStarting ComProc // 76
}

type iCoreWebView2_4 struct {
	vtbl *iCoreWebView2_4Vtbl
}

// QueryInterface4 upgrades an ICoreWebView2 to ICoreWebView2_4.
func (w *iCoreWebView2) QueryInterface4() *iCoreWebView2_4 {
	var out *iCoreWebView2_4
	w.vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(&iidCoreWebView2_4)),
		uintptr(unsafe.Pointer(&out)),
	)
	return out
}

func (w *iCoreWebView2_4) AddDownloadStarting(
	handler *iCoreWebView2DownloadStartingEventHandler,
	outToken *eventToken,
) uintptr {
	r, _, _ := w.vtbl.AddDownloadStarting.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(outToken)),
	)
	return r
}

func (w *iCoreWebView2_4) Release() {
	w.vtbl.Release.Call(uintptr(unsafe.Pointer(w)))
}

// --- ICoreWebView2DownloadStartingEventArgs --------------------------------

type iCoreWebView2DownloadStartingEventArgs struct {
	vtbl *iCoreWebView2DownloadStartingEventArgsVtbl
}

// Vtable order from WebView2.h ICoreWebView2DownloadStartingEventArgs.
type iCoreWebView2DownloadStartingEventArgsVtbl struct {
	_IUnknownVtbl
	GetDownloadOperation ComProc // 3
	GetCancel            ComProc // 4
	PutCancel            ComProc // 5
	GetResultFilePath    ComProc // 6
	PutResultFilePath    ComProc // 7
	GetHandled           ComProc // 8
	PutHandled           ComProc // 9
	GetDeferral          ComProc // 10
}

func (a *iCoreWebView2DownloadStartingEventArgs) PutResultFilePath(path string) uintptr {
	p := utf16PtrFromStr(path)
	r, _, _ := a.vtbl.PutResultFilePath.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(p)),
	)
	runtime.KeepAlive(p)
	return r
}

func (a *iCoreWebView2DownloadStartingEventArgs) PutCancel(cancel bool) uintptr {
	v := uintptr(0)
	if cancel {
		v = 1
	}
	r, _, _ := a.vtbl.PutCancel.Call(uintptr(unsafe.Pointer(a)), v)
	return r
}

func (a *iCoreWebView2DownloadStartingEventArgs) GetDownloadOperation() *iCoreWebView2DownloadOperation {
	var op *iCoreWebView2DownloadOperation
	a.vtbl.GetDownloadOperation.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&op)),
	)
	return op
}

func (a *iCoreWebView2DownloadStartingEventArgs) GetDeferral() *iCoreWebView2Deferral {
	var def *iCoreWebView2Deferral
	a.vtbl.GetDeferral.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&def)),
	)
	return def
}

// --- ICoreWebView2DownloadOperation ----------------------------------------

type iCoreWebView2DownloadOperation struct {
	vtbl *iCoreWebView2DownloadOperationVtbl
}

// Vtable order from WebView2.h ICoreWebView2DownloadOperation.
type iCoreWebView2DownloadOperationVtbl struct {
	_IUnknownVtbl
	AddBytesReceivedChanged       ComProc // 3
	RemoveBytesReceivedChanged    ComProc // 4
	AddEstimatedEndTimeChanged    ComProc // 5
	RemoveEstimatedEndTimeChanged ComProc // 6
	AddStateChanged               ComProc // 7
	RemoveStateChanged            ComProc // 8
	GetUri                        ComProc // 9
	GetContentDisposition         ComProc // 10
	GetMimeType                   ComProc // 11
	GetTotalBytesToReceive        ComProc // 12
	GetBytesReceived              ComProc // 13
	GetEstimatedEndTime           ComProc // 14
	GetResultFilePath             ComProc // 15
	GetState                      ComProc // 16
	GetInterruptReason            ComProc // 17
	Cancel                        ComProc // 18
	Pause                         ComProc // 19
	Resume                        ComProc // 20
	GetCanResume                  ComProc // 21
}

func (o *iCoreWebView2DownloadOperation) GetUri() string {
	var pwstr *uint16
	o.vtbl.GetUri.Call(
		uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(&pwstr)),
	)
	if pwstr == nil {
		return ""
	}
	s := wideToString(pwstr)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwstr)))
	return s
}

func (o *iCoreWebView2DownloadOperation) GetContentDisposition() string {
	var pwstr *uint16
	o.vtbl.GetContentDisposition.Call(
		uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(&pwstr)),
	)
	if pwstr == nil {
		return ""
	}
	s := wideToString(pwstr)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwstr)))
	return s
}

func (o *iCoreWebView2DownloadOperation) Release() {
	o.vtbl.Release.Call(uintptr(unsafe.Pointer(o)))
}

// --- ICoreWebView2DownloadStartingEventHandler (COM callback) --------------

type iCoreWebView2DownloadStartingEventHandler struct {
	vtbl *iCoreWebView2DownloadStartingEventHandlerVtbl
	impl downloadStartingImpl
}

type iCoreWebView2DownloadStartingEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type downloadStartingImpl interface {
	InvokeDownloadStarting(sender *iCoreWebView2, args *iCoreWebView2DownloadStartingEventArgs) uintptr
}

var downloadStartingVtblSingleton = iCoreWebView2DownloadStartingEventHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(downloadStartingQueryInterface),
		AddRef:         NewComProc(comAddRef),
		Release:        NewComProc(comRelease),
	},
	Invoke: NewComProc(downloadStartingInvoke),
}

func downloadStartingQueryInterface(this *iCoreWebView2DownloadStartingEventHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	comAddRef((*_IUnknown)(unsafe.Pointer(this)))
	return S_OK
}

func downloadStartingInvoke(this *iCoreWebView2DownloadStartingEventHandler, sender *iCoreWebView2, args *iCoreWebView2DownloadStartingEventArgs) uintptr {
	return this.impl.InvokeDownloadStarting(sender, args)
}

func newDownloadStartingHandler(impl downloadStartingImpl) *iCoreWebView2DownloadStartingEventHandler {
	return &iCoreWebView2DownloadStartingEventHandler{
		vtbl: &downloadStartingVtblSingleton,
		impl: impl,
	}
}

// --- filename helpers ------------------------------------------------------

func filenameFromDisposition(cd string) string {
	for _, part := range strings.Split(cd, ";") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(strings.ToLower(part), "filename=") {
			continue
		}
		name := strings.TrimSpace(part[len("filename="):])
		name = strings.Trim(name, `"`)
		return name
	}
	return ""
}

func filenameFromURL(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	return u
}
