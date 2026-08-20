//go:build windows

package windows

import (
	"runtime"
	"unsafe"
)

// iCoreWebView2 is the COM interface for the core WebView2 web content.
type iCoreWebView2 struct {
	vtbl *iCoreWebView2Vtbl
}

// Vtable layout verified against WebView2 SDK WebView2.h ICoreWebView2Vtbl.
// Indices 0-29 match the original spec. CapturePreview(30) and Reload(31)
// were missing from the original implementation; PostWebMessageAsJson(32)
// comes before PostWebMessageAsString(33); add_WebMessageReceived is at 34.
type iCoreWebView2Vtbl struct {
	_IUnknownVtbl
	GetSettings                            ComProc // 3
	GetSource                              ComProc // 4
	Navigate                               ComProc // 5
	NavigateToString                       ComProc // 6
	AddNavigationStarting                  ComProc // 7
	RemoveNavigationStarting               ComProc // 8
	AddContentLoading                      ComProc // 9
	RemoveContentLoading                   ComProc // 10
	AddSourceChanged                       ComProc // 11
	RemoveSourceChanged                    ComProc // 12
	AddHistoryChanged                      ComProc // 13
	RemoveHistoryChanged                   ComProc // 14
	AddNavigationCompleted                 ComProc // 15
	RemoveNavigationCompleted              ComProc // 16
	AddFrameNavigationStarting             ComProc // 17
	RemoveFrameNavigationStarting          ComProc // 18
	AddFrameNavigationCompleted            ComProc // 19
	RemoveFrameNavigationCompleted         ComProc // 20
	AddScriptDialogOpening                 ComProc // 21
	RemoveScriptDialogOpening              ComProc // 22
	AddPermissionRequested                 ComProc // 23
	RemovePermissionRequested              ComProc // 24
	AddProcessFailed                       ComProc // 25
	RemoveProcessFailed                    ComProc // 26
	AddScriptToExecuteOnDocumentCreated    ComProc // 27
	RemoveScriptToExecuteOnDocumentCreated ComProc // 28
	ExecuteScript                          ComProc // 29
	CapturePreview                         ComProc // 30
	Reload                                 ComProc // 31
	PostWebMessageAsJSON                   ComProc // 32
	PostWebMessageAsString                 ComProc // 33
	AddWebMessageReceived                  ComProc // 34
	RemoveWebMessageReceived               ComProc // 35
	CallDevToolsProtocolMethod             ComProc // 36
	GetBrowserProcessId                    ComProc // 37
	GetCanGoBack                           ComProc // 38
	GetCanGoForward                        ComProc // 39
	GoBack                                 ComProc // 40
	GoForward                              ComProc // 41
	GetDevToolsProtocolEventReceiver       ComProc // 42
	Stop                                   ComProc // 43
	AddNewWindowRequested                  ComProc // 44
	RemoveNewWindowRequested               ComProc // 45
	AddDocumentTitleChanged                ComProc // 46
	RemoveDocumentTitleChanged             ComProc // 47
	GetDocumentTitle                       ComProc // 48
	AddHostObjectToScript                  ComProc // 49
	RemoveHostObjectFromScript             ComProc // 50
	OpenDevToolsWindow                     ComProc // 51
	AddContainsFullScreenElementChanged    ComProc // 52
	RemoveContainsFullScreenElementChanged ComProc // 53
	GetContainsFullScreenElement           ComProc // 54
	AddWebResourceRequested                ComProc // 55
	RemoveWebResourceRequested             ComProc // 56
	AddWebResourceRequestedFilter          ComProc // 57
	RemoveWebResourceRequestedFilter       ComProc // 58
	AddWindowCloseRequested                ComProc // 59
	RemoveWindowCloseRequested             ComProc // 60
}

func (w *iCoreWebView2) Navigate(url string) uintptr {
	p := utf16PtrFromStr(url)
	r, _, _ := w.vtbl.Navigate.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(p)),
	)
	runtime.KeepAlive(p)
	return r
}

func (w *iCoreWebView2) NavigateToString(html string) uintptr {
	p := utf16PtrFromStr(html)
	r, _, _ := w.vtbl.NavigateToString.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(p)),
	)
	runtime.KeepAlive(p)
	return r
}

func (w *iCoreWebView2) ExecuteScript(js string, handler uintptr) uintptr {
	p := utf16PtrFromStr(js)
	r, _, _ := w.vtbl.ExecuteScript.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(p)),
		handler,
	)
	runtime.KeepAlive(p)
	return r
}

func (w *iCoreWebView2) AddScriptToExecuteOnDocumentCreated(js string, handler uintptr) uintptr {
	p := utf16PtrFromStr(js)
	r, _, _ := w.vtbl.AddScriptToExecuteOnDocumentCreated.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(p)),
		handler,
	)
	runtime.KeepAlive(p)
	return r
}

func (w *iCoreWebView2) AddWebMessageReceived(
	handler *iCoreWebView2WebMessageReceivedEventHandler,
	outToken *eventToken,
) uintptr {
	r, _, _ := w.vtbl.AddWebMessageReceived.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(outToken)),
	)
	return r
}

func (w *iCoreWebView2) AddNavigationCompleted(
	handler *iCoreWebView2NavigationCompletedEventHandler,
	outToken *eventToken,
) uintptr {
	r, _, _ := w.vtbl.AddNavigationCompleted.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(outToken)),
	)
	return r
}

func (w *iCoreWebView2) PostWebMessageAsString(msg string) uintptr {
	p := utf16PtrFromStr(msg)
	r, _, _ := w.vtbl.PostWebMessageAsString.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(p)),
	)
	runtime.KeepAlive(p)
	return r
}

func (w *iCoreWebView2) AddPermissionRequested(
	handler *iCoreWebView2PermissionRequestedEventHandler,
	outToken *eventToken,
) uintptr {
	r, _, _ := w.vtbl.AddPermissionRequested.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(outToken)),
	)
	return r
}

func (w *iCoreWebView2) Release() {
	w.vtbl.Release.Call(uintptr(unsafe.Pointer(w)))
}

func (w *iCoreWebView2) GetSettings() *iCoreWebView2Settings {
	var s *iCoreWebView2Settings
	w.vtbl.GetSettings.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(&s)),
	)
	return s
}

func (w *iCoreWebView2) PutIsStatusBarEnabled(enabled bool) uintptr {
	settings := w.GetSettings()
	if settings == nil {
		return 0
	}
	v := uintptr(0)
	if enabled {
		v = 1
	}
	r, _, _ := settings.vtbl.PutIsStatusBarEnabled.Call(
		uintptr(unsafe.Pointer(settings)), v,
	)
	return r
}

func (w *iCoreWebView2) PutAreDevToolsEnabled(enabled bool) uintptr {
	settings := w.GetSettings()
	if settings == nil {
		return 0
	}
	v := uintptr(0)
	if enabled {
		v = 1
	}
	r, _, _ := settings.vtbl.PutAreDevToolsEnabled.Call(
		uintptr(unsafe.Pointer(settings)), v,
	)
	return r
}

type iCoreWebView2Settings struct {
	vtbl *iCoreWebView2SettingsVtbl
}

type iCoreWebView2SettingsVtbl struct {
	_IUnknownVtbl
	GetIsScriptEnabled                ComProc
	PutIsScriptEnabled                ComProc
	GetIsWebMessageEnabled            ComProc
	PutIsWebMessageEnabled            ComProc
	GetAreDefaultScriptDialogsEnabled ComProc
	PutAreDefaultScriptDialogsEnabled ComProc
	GetIsStatusBarEnabled             ComProc
	PutIsStatusBarEnabled             ComProc
	GetAreDevToolsEnabled             ComProc
	PutAreDevToolsEnabled             ComProc
	GetAreDefaultContextMenusEnabled  ComProc
	PutAreDefaultContextMenusEnabled  ComProc
	GetAreHostObjectsAllowed          ComProc
	PutAreHostObjectsAllowed          ComProc
	GetIsBuiltInErrorPageEnabled      ComProc
	PutIsBuiltInErrorPageEnabled      ComProc
}
