//go:build windows

package windows

import "unsafe"

// iCoreWebView2Controller is the COM interface for the WebView2 controller.
type iCoreWebView2Controller struct {
	vtbl *iCoreWebView2ControllerVtbl
}

type iCoreWebView2ControllerVtbl struct {
	_IUnknownVtbl
	GetIsVisible                      ComProc
	PutIsVisible                      ComProc
	GetBounds                         ComProc
	PutBounds                         ComProc
	GetZoomFactor                     ComProc
	PutZoomFactor                     ComProc
	AddZoomFactorChanged              ComProc
	RemoveZoomFactorChanged           ComProc
	SetBoundsAndZoomFactor            ComProc
	MoveFocus                         ComProc
	AddMoveFocusRequested             ComProc
	RemoveMoveFocusRequested          ComProc
	AddGotFocus                       ComProc
	RemoveGotFocus                    ComProc
	AddLostFocus                      ComProc
	RemoveLostFocus                   ComProc
	AddAcceleratorKeyPressed          ComProc
	RemoveAcceleratorKeyPressed       ComProc
	GetParentWindow                   ComProc
	PutParentWindow                   ComProc
	NotifyParentWindowPositionChanged ComProc
	Close                             ComProc
	GetCoreWebView2                   ComProc
}

func (c *iCoreWebView2Controller) PutIsVisible(visible bool) uintptr {
	v := uintptr(0)
	if visible {
		v = 1
	}
	r, _, _ := c.vtbl.PutIsVisible.Call(uintptr(unsafe.Pointer(c)), v)
	return r
}

func (c *iCoreWebView2Controller) PutBounds(bounds RECT) uintptr {
	r, _, _ := c.vtbl.PutBounds.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&bounds)),
	)
	return r
}

func (c *iCoreWebView2Controller) PutParentWindow(hwnd uintptr) uintptr {
	r, _, _ := c.vtbl.PutParentWindow.Call(uintptr(unsafe.Pointer(c)), hwnd)
	return r
}

func (c *iCoreWebView2Controller) GetCoreWebView2(out **iCoreWebView2) uintptr {
	r, _, _ := c.vtbl.GetCoreWebView2.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(out)),
	)
	return r
}

func (c *iCoreWebView2Controller) Close() uintptr {
	r, _, _ := c.vtbl.Close.Call(uintptr(unsafe.Pointer(c)))
	return r
}

func (c *iCoreWebView2Controller) Release() {
	c.vtbl.Release.Call(uintptr(unsafe.Pointer(c)))
}
