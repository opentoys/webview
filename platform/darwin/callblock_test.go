//go:build darwin

package darwin

import (
	"testing"

	"github.com/ebitengine/purego/objc"
)

// TestCallBlockWithID verifies callBlock works with objc.ID (pointer) args.
// This mirrors the open-panel native path: callBlock(completion, urls-or-nil).
func TestCallBlockWithID(t *testing.T) {
	// Build a real ObjC block via objc.NewBlock. The invoke pointer is a
	// purego.NewCallback-generated C function, which callBlock calls via
	// SyscallN. This confirms the SyscallN path works for both Go-created
	// and WebKit-created blocks.
	var got uintptr
	block := objc.NewBlock(func(b objc.Block, val uintptr) {
		got = val
	})
	defer block.Release()

	// callBlock with a uintptr arg (simulates passing objc.ID).
	callBlock(objc.ID(block), uintptr(42))
	if got != 42 {
		t.Fatalf("callBlock(block, 42): got %d, want 42", got)
	}
}

// TestCallBlockNil verifies callBlock with objc.ID(0) as block is a no-op.
func TestCallBlockNil(t *testing.T) {
	r := callBlock(objc.ID(0), uintptr(0))
	if r != 0 {
		t.Fatalf("callBlock(nil) = %d, want 0", r)
	}
}
