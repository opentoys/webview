//go:build darwin

package darwin

import (
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// cgRect is the layout of a CoreGraphics CGRect (x, y, width, height as CGFloat).
// Passed by value to objc Send; purego marshals struct args directly.
type cgRect struct {
	X, Y, W, H float64
}

func rect(x, y, w, h float64) cgRect {
	return cgRect{X: x, Y: y, W: w, H: h}
}

// nsString converts a Go string to an autoreleased NSString.
func nsString(s string) objc.ID {
	return objc.ID(nsStringClass).Send(stringWithUTF8Sel, s)
}

// goString copies an NSString's UTF-8 bytes into a Go string. UTF8String
// returns a const char* owned by the NSString (valid only until the current
// autorelease drain); the string(...) conversion copies, so the returned Go
// string is safe afterwards. Never hold the pointer.
func goString(id objc.ID) string {
	if id == 0 {
		return ""
	}
	ptr := objc.Send[unsafe.Pointer](id, UTF8StringSel)
	if ptr == nil {
		return ""
	}
	// Walk to NUL.
	n := 0
	for (*[1 << 30]byte)(unsafe.Pointer(ptr))[n] != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n))
}
