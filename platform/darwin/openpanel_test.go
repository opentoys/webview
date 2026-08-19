//go:build darwin

package darwin

import (
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

// TestPathURLs verifies pathURLs builds an NSArray<NSURL> from absolute paths,
// and returns nil (0) for an empty list.
func TestPathURLs(t *testing.T) {
	if arr := pathURLs(nil); arr != 0 {
		t.Fatalf("pathURLs(nil) = %#x, want 0", uintptr(arr))
	}
	if arr := pathURLs([]string{}); arr != 0 {
		t.Fatalf("pathURLs(empty) = %#x, want 0", uintptr(arr))
	}

	arr := pathURLs([]string{"/tmp/a.txt", "/tmp/b.bin"})
	if arr == 0 {
		t.Fatal("pathURLs(2) = nil, want array")
	}
	if count := objc.ID(arr).Send(objc.RegisterName("count")); count != 2 {
		t.Fatalf("array count = %d, want 2", count)
	}
	first := objc.ID(arr).Send(objc.RegisterName("objectAtIndex:"), 0)
	if first == 0 {
		t.Fatal("element 0 is nil")
	}
	if objc.ID(first).Send(objc.RegisterName("isKindOfClass:"), nsURLClass) == 0 {
		t.Fatal("element 0 is not an NSURL")
	}
}

// TestOpenPanelResult verifies openPanelResult maps (paths, ok) to the
// NSArray<NSURL> value or nil.
func TestOpenPanelResult(t *testing.T) {
	if r := openPanelResult(nil, false); r != 0 {
		t.Fatalf("cancel result = %#x, want 0", uintptr(r))
	}
	if r := openPanelResult([]string{"/x"}, false); r != 0 {
		t.Fatalf("cancel-with-paths result = %#x, want 0", uintptr(r))
	}
	r := openPanelResult([]string{"/x"}, true)
	if r == 0 {
		t.Fatal("ok result = nil, want array")
	}
}

// TestOpenPanelFuncRouting verifies showOpenPanel routes to OpenPanelFunc when
// set, without touching the native panel.
func TestOpenPanelFuncRouting(t *testing.T) {
	p := New()
	called := make(chan openPanelParams, 1)
	p.OpenPanelFunc = func(params openPanelParams, cb func([]string, bool)) {
		called <- params
	}
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	waitWindow(t, p)

	mainThread(func() {
		p.showOpenPanel(openPanelParams{AllowsMultipleSelection: true}, objc.Block(0))
	})
	select {
	case params := <-called:
		if !params.AllowsMultipleSelection {
			t.Fatal("routed params lost AllowsMultipleSelection")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OpenPanelFunc never called")
	}
	p.Close()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() = %v", err)
	}
}

// waitWindow blocks until p.window is non-zero or the deadline passes.
func waitWindow(t *testing.T, p *Platform) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.mu.Lock()
		w := p.window
		p.mu.Unlock()
		if w != 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("window never created")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
