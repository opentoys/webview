//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"
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
	return ComProc(syscall.NewCallback(fn))
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

// String formats the GUID as "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx".
func (g *GUID) String() string {
	return fmt.Sprintf("%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3],
		g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

// _IUnknownVtbl is the vtable base for all COM interfaces.
type _IUnknownVtbl struct {
	QueryInterface ComProc
	AddRef         ComProc
	Release        ComProc
}

// utf16PtrFromStr converts a Go string to a *uint16 for Windows APIs.
// Caller MUST call runtime.KeepAlive(p) after the syscall to prevent GC.
func utf16PtrFromStr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// wideToString converts a COM-allocated *uint16 (NUL-terminated UTF-16) to a
// Go string. Returns "" for a nil pointer. The caller must CoTaskMemFree the
// pointer after this call if the COM API allocated it.
func wideToString(p *uint16) string {
	if p == nil {
		return ""
	}
	// Find the NUL terminator.
	n := 0
	q := p
	for *q != 0 {
		n++
		q = (*uint16)(unsafe.Add(unsafe.Pointer(q), 2))
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// S_OK
const S_OK = 0

// COINIT values for CoInitializeEx.
const COINIT_APARTMENTTHREADED = 0x2
