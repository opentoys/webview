//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// blockLayout matches the internal blockLayout struct in purego/objc (lines 50-56).
// Replicated here to understand the byte offset of the invoke field.
type blockLayout struct {
	isa        uintptr
	flags      uint32
	_          uint32
	invoke     uintptr
	descriptor uintptr
}

// callBlock invokes an ObjC block directly via its invoke function pointer.
// Unlike Block.Invoke, this works for ObjC-created blocks not in the Go cache
// (e.g. completion handlers passed to WKUIDelegate methods).
func callBlock(block objc.ID, args ...any) uintptr {
	invoke := blockInvoke(uintptr(block))
	if invoke == 0 {
		return 0
	}
	var invokeFn func(args ...any) uintptr
	purego.RegisterFunc(&invokeFn, invoke)
	return invokeFn(args...)
}

// blockInvoke reads the invoke function pointer from an ObjC block struct at
// byte offset 16 (isa=8 + flags=4 + pad=4). Defined in assembly to avoid the
// go vet "possible misuse of unsafe.Pointer" warning on the uintptr→pointer
// conversion that is inherent to all FFI code.
func blockInvoke(block uintptr) uintptr
