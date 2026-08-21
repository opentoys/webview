//go:build windows

package windows

import "unsafe"

// --- Environment completed handler ---

type iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler struct {
	vtbl *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandlerVtbl
	impl envCompletedImpl
}

type iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type envCompletedImpl interface {
	InvokeEnvCompleted(errorCode uintptr, env *iCoreWebView2Environment) uintptr
}

var envCompletedVtblSingleton = iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(envCompletedQueryInterface),
		AddRef:         NewComProc(comAddRef),
		Release:        NewComProc(comRelease),
	},
	Invoke: NewComProc(envCompletedInvoke),
}

func envCompletedQueryInterface(this *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	comAddRef((*_IUnknown)(unsafe.Pointer(this)))
	return S_OK
}

func envCompletedInvoke(this *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler, errorCode uintptr, env *iCoreWebView2Environment) uintptr {
	return this.impl.InvokeEnvCompleted(errorCode, env)
}

func newEnvCompletedHandler(impl envCompletedImpl) *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler {
	return &iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler{
		vtbl: &envCompletedVtblSingleton,
		impl: impl,
	}
}

// --- Controller completed handler ---

type iCoreWebView2CreateCoreWebView2ControllerCompletedHandler struct {
	vtbl *iCoreWebView2CreateCoreWebView2ControllerCompletedHandlerVtbl
	impl controllerCompletedImpl
}

type iCoreWebView2CreateCoreWebView2ControllerCompletedHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type controllerCompletedImpl interface {
	InvokeControllerCompleted(errorCode uintptr, controller *iCoreWebView2Controller) uintptr
}

var controllerCompletedVtblSingleton = iCoreWebView2CreateCoreWebView2ControllerCompletedHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(controllerCompletedQueryInterface),
		AddRef:         NewComProc(comAddRef),
		Release:        NewComProc(comRelease),
	},
	Invoke: NewComProc(controllerCompletedInvoke),
}

func controllerCompletedQueryInterface(this *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	comAddRef((*_IUnknown)(unsafe.Pointer(this)))
	return S_OK
}

func controllerCompletedInvoke(this *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler, errorCode uintptr, controller *iCoreWebView2Controller) uintptr {
	return this.impl.InvokeControllerCompleted(errorCode, controller)
}

func newControllerCompletedHandler(impl controllerCompletedImpl) *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler {
	return &iCoreWebView2CreateCoreWebView2ControllerCompletedHandler{
		vtbl: &controllerCompletedVtblSingleton,
		impl: impl,
	}
}

// --- Web message received handler ---

type iCoreWebView2WebMessageReceivedEventHandler struct {
	vtbl *iCoreWebView2WebMessageReceivedEventHandlerVtbl
	impl webMessageReceivedImpl
}

type iCoreWebView2WebMessageReceivedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type webMessageReceivedImpl interface {
	InvokeWebMessageReceived(sender *iCoreWebView2, args *iCoreWebView2WebMessageReceivedEventArgs) uintptr
}

var webMessageReceivedVtblSingleton = iCoreWebView2WebMessageReceivedEventHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(webMessageReceivedQueryInterface),
		AddRef:         NewComProc(comAddRef),
		Release:        NewComProc(comRelease),
	},
	Invoke: NewComProc(webMessageReceivedInvoke),
}

func webMessageReceivedQueryInterface(this *iCoreWebView2WebMessageReceivedEventHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	comAddRef((*_IUnknown)(unsafe.Pointer(this)))
	return S_OK
}

func webMessageReceivedInvoke(this *iCoreWebView2WebMessageReceivedEventHandler, sender *iCoreWebView2, args *iCoreWebView2WebMessageReceivedEventArgs) uintptr {
	return this.impl.InvokeWebMessageReceived(sender, args)
}

func newWebMessageReceivedHandler(impl webMessageReceivedImpl) *iCoreWebView2WebMessageReceivedEventHandler {
	return &iCoreWebView2WebMessageReceivedEventHandler{
		vtbl: &webMessageReceivedVtblSingleton,
		impl: impl,
	}
}

// --- Permission requested handler ---

var permissionRequestedVtblSingleton = iCoreWebView2PermissionRequestedEventHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(permissionRequestedQueryInterface),
		AddRef:         NewComProc(comAddRef),
		Release:        NewComProc(comRelease),
	},
	Invoke: NewComProc(permissionRequestedInvoke),
}

func permissionRequestedQueryInterface(this *iCoreWebView2PermissionRequestedEventHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	comAddRef((*_IUnknown)(unsafe.Pointer(this)))
	return S_OK
}

func permissionRequestedInvoke(this *iCoreWebView2PermissionRequestedEventHandler, sender *iCoreWebView2, args uintptr) uintptr {
	return this.impl.InvokePermissionRequested(sender, args)
}

func newPermissionRequestedHandler(impl permissionRequestedImpl) *iCoreWebView2PermissionRequestedEventHandler {
	return &iCoreWebView2PermissionRequestedEventHandler{
		vtbl: &permissionRequestedVtblSingleton,
		impl: impl,
	}
}

// --- Navigation completed handler ---

type iCoreWebView2NavigationCompletedEventHandler struct {
	vtbl *iCoreWebView2NavigationCompletedEventHandlerVtbl
	impl navigationCompletedImpl
}

type iCoreWebView2NavigationCompletedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type navigationCompletedImpl interface {
	InvokeNavigationCompleted(sender *iCoreWebView2, isSuccess bool) uintptr
}

var navigationCompletedVtblSingleton = iCoreWebView2NavigationCompletedEventHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(navigationCompletedQueryInterface),
		AddRef:         NewComProc(comAddRef),
		Release:        NewComProc(comRelease),
	},
	Invoke: NewComProc(navigationCompletedInvoke),
}

func navigationCompletedQueryInterface(this *iCoreWebView2NavigationCompletedEventHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	comAddRef((*_IUnknown)(unsafe.Pointer(this)))
	return S_OK
}

func navigationCompletedInvoke(this *iCoreWebView2NavigationCompletedEventHandler, sender *iCoreWebView2, args *iCoreWebView2NavigationCompletedEventArgs) uintptr {
	return this.impl.InvokeNavigationCompleted(sender, args.GetIsSuccess())
}

func newNavigationCompletedHandler(impl navigationCompletedImpl) *iCoreWebView2NavigationCompletedEventHandler {
	return &iCoreWebView2NavigationCompletedEventHandler{
		vtbl: &navigationCompletedVtblSingleton,
		impl: impl,
	}
}

// --- Shared COM helpers ---

type _IUnknown struct {
	vtbl *_IUnknownVtbl
}

func comAddRef(this *_IUnknown) uintptr {
	return 1 // singleton, never freed
}

func comRelease(this *_IUnknown) uintptr {
	return 1 // singleton, never freed
}
