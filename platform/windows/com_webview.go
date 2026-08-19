//go:build windows

package windows

import "unsafe"

// iCoreWebView2 is the COM interface for the core WebView2 web content.
type iCoreWebView2 struct {
	vtbl *iCoreWebView2Vtbl
}

type iCoreWebView2Vtbl struct {
	_IUnknownVtbl
	GetSettings                          ComProc
	GetSource                            ComProc
	Navigate                             ComProc
	NavigateToString                     ComProc
	AddNavigationStarting                ComProc
	RemoveNavigationStarting             ComProc
	AddContentLoading                    ComProc
	RemoveContentLoading                 ComProc
	AddSourceChanged                     ComProc
	RemoveSourceChanged                  ComProc
	AddHistoryChanged                    ComProc
	RemoveHistoryChanged                 ComProc
	AddNavigationCompleted               ComProc
	RemoveNavigationCompleted            ComProc
	AddFrameNavigationStarting           ComProc
	RemoveFrameNavigationStarting        ComProc
	AddFrameNavigationCompleted          ComProc
	RemoveFrameNavigationCompleted       ComProc
	AddScriptDialogOpening               ComProc
	RemoveScriptDialogOpening            ComProc
	AddPermissionRequested               ComProc
	RemovePermissionRequested            ComProc
	AddProcessFailed                     ComProc
	RemoveProcessFailed                  ComProc
	AddScriptToExecuteOnDocumentCreated  ComProc
	RemoveScriptToExecuteOnDocumentCreated ComProc
	ExecuteScript                        ComProc
	AddWebMessageReceived                ComProc
	RemoveWebMessageReceived             ComProc
	PostWebMessageAsString               ComProc
	PostWebMessageAsJSON                 ComProc
	_                                    [30]ComProc // padding for remaining slots
}

func (w *iCoreWebView2) Navigate(url string) uintptr {
	r, _, _ := w.vtbl.Navigate.Call(
		uintptr(unsafe.Pointer(w)),
		unsafeString(url),
	)
	return r
}

func (w *iCoreWebView2) NavigateToString(html string) uintptr {
	r, _, _ := w.vtbl.NavigateToString.Call(
		uintptr(unsafe.Pointer(w)),
		unsafeString(html),
	)
	return r
}

func (w *iCoreWebView2) ExecuteScript(js string, handler uintptr) uintptr {
	r, _, _ := w.vtbl.ExecuteScript.Call(
		uintptr(unsafe.Pointer(w)),
		unsafeString(js),
		handler,
	)
	return r
}

func (w *iCoreWebView2) AddScriptToExecuteOnDocumentCreated(js string, handler uintptr) uintptr {
	r, _, _ := w.vtbl.AddScriptToExecuteOnDocumentCreated.Call(
		uintptr(unsafe.Pointer(w)),
		unsafeString(js),
		handler,
	)
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

func (w *iCoreWebView2) PostWebMessageAsString(msg string) uintptr {
	r, _, _ := w.vtbl.PostWebMessageAsString.Call(
		uintptr(unsafe.Pointer(w)),
		unsafeString(msg),
	)
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
