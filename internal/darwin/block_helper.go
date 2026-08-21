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
//
// Uses purego.SyscallN (fixed-argument C ABI) instead of RegisterFunc with a
// variadic Go wrapper, which can misroute arguments on ARM64 Darwin when the
// actual C function is not variadic.
func callBlock(block objc.ID, args ...any) uintptr {
	if block == 0 {
		return 0
	}
	invoke := blockInvoke(uintptr(block))
	if invoke == 0 {
		return 0
	}
	uargs := make([]uintptr, len(args)+1)
	uargs[0] = uintptr(block)
	for i, a := range args {
		switch v := a.(type) {
		case uintptr:
			uargs[i+1] = v
		case objc.ID:
			uargs[i+1] = uintptr(v)
		case int64:
			uargs[i+1] = uintptr(v)
		case int:
			uargs[i+1] = uintptr(v)
		case uint64:
			uargs[i+1] = uintptr(v)
		case bool:
			if v {
				uargs[i+1] = 1
			}
		case objc.Block:
			uargs[i+1] = uintptr(v)
		default:
			panic("callBlock: unsupported arg type")
		}
	}
	r, _, _ := purego.SyscallN(invoke, uargs...)
	return r
}

// blockInvoke reads the invoke function pointer from an ObjC block struct at
// byte offset 16 (isa=8 + flags=4 + pad=4). Defined in assembly to avoid the
// go vet "possible misuse of unsafe.Pointer" warning on the uintptr→pointer
// conversion that is inherent to all FFI code.
func blockInvoke(block uintptr) uintptr
