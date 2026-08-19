//go:build windows

package windows

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/windows"
)

// ComProc stores a COM virtual function pointer.
type ComProc uintptr

// Call invokes the COM procedure.
func (p ComProc) Call(a ...uintptr) (r1, r2 uintptr, lastErr error) {
	return syscall.SyscallN(uintptr(p), a...)
}

// NewComProc wraps a Go function as a COM-callable callback via
// windows.NewCallback. The returned pointer is safe to store in vtables.
func NewComProc(fn interface{}) ComProc {
	return ComProc(windows.NewCallback(fn))
}

// GUID matches the Windows COM GUID layout.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// NewGUID parses a "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" string.
func NewGUID(s string) (*GUID, error) {
	var g GUID
	n, err := fmt.Sscanf(s, "%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		&g.Data1, &g.Data2, &g.Data3,
		&g.Data4[0], &g.Data4[1], &g.Data4[2], &g.Data4[3],
		&g.Data4[4], &g.Data4[5], &g.Data4[6], &g.Data4[7])
	if err != nil || n != 11 {
		return nil, fmt.Errorf("invalid GUID: %q", s)
	}
	return &g, nil
}

// _IUnknownVtbl is the vtable base for all COM interfaces.
type _IUnknownVtbl struct {
	QueryInterface ComProc
	AddRef         ComProc
	Release        ComProc
}

// Known COM GUIDs for WebView2.
var (
	IIDICoreWebView2CreateEnvironmentCompletedHandler, _ = NewGUID("4E19640C-8A27-4C64-9837-0C3C3A635570")
	IIDICoreWebView2CreateControllerCompletedHandler, _  = NewGUID("6C4819F3-C9B7-4496-81C0-1C68057E1C1A")
	IIDICoreWebView2WebMessageReceivedEventHandler, _   = NewGUID("57213F19-00E7-4F37-9A21-47D0B47BBA51")
	IIDICoreWebView2PermissionRequestedEventHandler, _   = NewGUID("15E1C6A3-C72A-4DF3-91D7-D097FBEC6BFD")
	ICoreWebView2EnvironmentIID, _                       = NewGUID("B96D755E-0319-4E92-A296-23436F46A1FC")
	ICoreWebView2ControllerIID, _                        = NewGUID("4D00C0D1-9434-4EB6-8078-818BE798743C")
	ICoreWebView2IID, _                                  = NewGUID("76ECEACB-0462-4D93-AC27-2CDFF26E526A")
)

// utf16PtrFromStr converts a Go string to a *uint16 for Windows APIs.
// Caller MUST call runtime.KeepAlive(p) after the syscall to prevent GC.
func utf16PtrFromStr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// S_OK
const S_OK = 0

// COINIT values for CoInitializeEx.
const (
	COINIT_APARTMENTTHREADED = 0x2
	COINIT_MULTITHREADED     = 0x0
)
