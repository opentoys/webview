//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/opentoys/webview/internal/types"
)

var applicationOnce sync.Once
var applicationReady = make(chan struct{})
var applicationDone = make(chan struct{})
var applicationErr error
var applicationHWND, applicationTID uintptr
var applicationDispatch dispatchQueue

func startApplication() error {
	applicationOnce.Do(func() {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer close(applicationDone)
			applicationTID, _, _ = pGetCurrentThreadId.Call()
			r, _, err := pCoInitializeEx.Call(0, COINIT_APARTMENTTHREADED)
			if r != S_OK && r != 1 {
				applicationErr = fmt.Errorf("webview: COM initialization: %w", err)
				close(applicationReady)
				return
			}
			defer pCoUninitialize.Call()
			proc := syscall.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
				if msg == WM_APP {
					if fn := applicationDispatch.pop(); fn != nil {
						fn()
					}
					return 0
				}
				r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
				return r
			})
			instance, _, _ := pGetModuleHandleW.Call(0)
			name := utf16PtrFromStr("GoWebviewApplication")
			wc := WNDCLASSEXW{CbSize: uint32(unsafe.Sizeof(WNDCLASSEXW{})), LpfnWndProc: proc, HInstance: instance, LpszClassName: name}
			pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
			applicationHWND, _, err = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, 0, 0, instance, 0)
			runtime.KeepAlive(name)
			if applicationHWND == 0 {
				applicationErr = fmt.Errorf("webview: application host: %w", err)
			}
			close(applicationReady)
			if applicationErr != nil {
				return
			}
			var msg MSG
			for {
				r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
				if r == 0 || int32(r) == -1 {
					break
				}
				pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
				pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
			}
			pDestroyWindow.Call(applicationHWND)
		}()
	})
	<-applicationReady
	return applicationErr
}
func applicationThread(fn func()) {
	select {
	case <-applicationDone:
		return
	default:
	}
	tid, _, _ := pGetCurrentThreadId.Call()
	if tid == applicationTID {
		fn()
		return
	}
	done := make(chan struct{})
	applicationDispatch.push(func() { fn(); close(done) })
	pPostMessageW.Call(applicationHWND, WM_APP, 0, 0)
	select {
	case <-done:
	case <-applicationDone:
	}
}
func StopApplication() error {
	select {
	case <-applicationReady:
	default:
		return nil
	}
	if applicationErr != nil {
		return nil
	}
	applicationThread(func() { pPostQuitMessage.Call(0) })
	tid, _, _ := pGetCurrentThreadId.Call()
	if tid != applicationTID {
		<-applicationDone
	}
	return nil
}
func (p *Platform) SetCloseRequest(fn func()) { p.closeRequest = fn }
func (p *Platform) Start() error {
	if err := startApplication(); err != nil {
		return err
	}
	var err error
	applicationThread(func() { err = p.setup() })
	if err != nil {
		return err
	}
	select {
	case err = <-p.initDone:
	case <-applicationDone:
		err = fmt.Errorf("webview: application stopped during initialization")
	}
	return err
}
func (p *Platform) Show() error {
	applicationThread(func() {
		p.hidden = false
		if p.hwnd != 0 {
			pShowWindow.Call(p.hwnd, SW_SHOW)
			pSetForegroundWindow.Call(p.hwnd)
		}
	})
	return nil
}
func (p *Platform) Hidden() error {
	applicationThread(func() {
		p.hidden = true
		if p.hwnd != 0 {
			pShowWindow.Call(p.hwnd, 0)
		}
	})
	return nil
}

type Tray struct{ owner *Platform }

func NewTray(opts types.SystrayOptions) (*Tray, error) {
	if err := startApplication(); err != nil {
		return nil, err
	}
	p := &Platform{trayOptions: &opts}
	var err error
	applicationThread(func() {
		instance, _, _ := pGetModuleHandleW.Call(0)
		name := registerNativeClass()
		p.hwnd, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, 0, 0, instance, 0)
		runtime.KeepAlive(name)
		if p.hwnd == 0 {
			err = types.ErrMenuUnsupported
			return
		}
		nativeTrayOwners[p.hwnd] = p
		taskbar := utf16PtrFromStr("TaskbarCreated")
		p.taskbarCreated, _, _ = registerTrayMessage.Call(uintptr(unsafe.Pointer(taskbar)))
		runtime.KeepAlive(taskbar)
		err = p.applySystray()
	})
	if err != nil {
		applicationThread(func() {
			p.removeSystray()
			if p.hwnd != 0 {
				pDestroyWindow.Call(p.hwnd)
			}
		})
		return nil, err
	}
	return &Tray{p}, nil
}
func (p *Platform) trayWndproc(hwnd, msg, wp, lp uintptr) uintptr {
	if p.taskbarCreated != 0 && msg == p.taskbarCreated {
		p.trayAdded = false
		_ = p.applySystray()
		return 0
	}
	if msg == trayMessage {
		if lp == 0x202 { // WM_LBUTTONUP
			if p.trayOptions != nil && p.trayOptions.OnClick != nil {
				p.trayOptions.OnClick()
			} else {
				p.showTrayMenu()
			}
		} else if lp == 0x205 {
			p.showTrayMenu()
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}
func (t *Tray) Update(opts types.SystrayOptions) error {
	var err error
	applicationThread(func() { t.owner.trayOptions = &opts; err = t.owner.applySystray() })
	return err
}
func (t *Tray) Close() error {
	applicationThread(func() { t.owner.removeSystray(); pDestroyWindow.Call(t.owner.hwnd); t.owner.hwnd = 0 })
	return nil
}

func (p *Platform) SetInitialHidden(hidden bool) { p.hidden = hidden }

// A single Win32 class routes through live HWNDs. syscall callbacks cannot be
// freed, so allocating one callback/class per object would retain every closed
// window and tray for the rest of the process.
var nativeClassOnce sync.Once
var nativeClassName = utf16PtrFromStr("GoWebviewNativeObject")
var nativeWindowOwners = map[uintptr]*Platform{}
var nativeTrayOwners = map[uintptr]*Platform{}

func registerNativeClass() *uint16 {
	nativeClassOnce.Do(func() {
		proc := syscall.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
			var result uintptr
			if p := nativeWindowOwners[hwnd]; p != nil {
				result = p.wndproc(hwnd, msg, wp, lp)
			} else if p := nativeTrayOwners[hwnd]; p != nil {
				result = p.trayWndproc(hwnd, msg, wp, lp)
			} else {
				result, _, _ = pDefWindowProcW.Call(hwnd, msg, wp, lp)
			}
			if msg == 0x82 {
				delete(nativeWindowOwners, hwnd)
				delete(nativeTrayOwners, hwnd)
			} // WM_NCDESTROY
			return result
		})
		instance, _, _ := pGetModuleHandleW.Call(0)
		wc := WNDCLASSEXW{CbSize: uint32(unsafe.Sizeof(WNDCLASSEXW{})), LpfnWndProc: proc, HInstance: instance, LpszClassName: nativeClassName}
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})
	return nativeClassName
}

func WaitApplication() {
	select {
	case <-applicationReady:
		<-applicationDone
	default:
	}
}
