//go:build darwin

package darwin

import "testing"

func TestSetSizeBeforeRun(t *testing.T) {
	p := &Platform{}
	p.SetSize(640, 480, SizeFixed)

	if p.pendingW != 640 || p.pendingH != 480 {
		t.Fatalf("pending size = %dx%d, want 640x480", p.pendingW, p.pendingH)
	}
	if p.pendingSizeHint != SizeFixed {
		t.Fatalf("pending hint = %v, want SizeFixed", p.pendingSizeHint)
	}
	if !p.hasPendingSize {
		t.Fatal("pending size was not marked for setup")
	}
}

func TestSetSizeRejectsInvalidDimensions(t *testing.T) {
	p := &Platform{}
	p.SetSize(0, 480, SizeNone)
	p.SetSize(640, -1, SizeNone)
	if p.hasPendingSize {
		t.Fatal("invalid dimensions must not update pending size")
	}
}
