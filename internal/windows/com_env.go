//go:build windows

package windows

import "unsafe"

// iCoreWebView2Environment is the COM interface for the WebView2 environment.
type iCoreWebView2Environment struct {
	vtbl *iCoreWebView2EnvironmentVtbl
}

type iCoreWebView2EnvironmentVtbl struct {
	_IUnknownVtbl
	CreateCoreWebView2Controller            ComProc
	CreateWebResourceResponse               ComProc
	GetBrowserVersionString                 ComProc
	AddNewBrowserVersionAvailable           ComProc
	RemoveNewBrowserVersionAvailable        ComProc
	CreateCoreWebView2CompositionController ComProc
	CreateCoreWebView2ControllerWithOptions ComProc
}

func (e *iCoreWebView2Environment) CreateCoreWebView2Controller(
	parentHWND uintptr,
	handler *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler,
) uintptr {
	r, _, _ := e.vtbl.CreateCoreWebView2Controller.Call(
		uintptr(unsafe.Pointer(e)),
		parentHWND,
		uintptr(unsafe.Pointer(handler)),
	)
	return r
}

func (e *iCoreWebView2Environment) Release() {
	e.vtbl.Release.Call(uintptr(unsafe.Pointer(e)))
}
