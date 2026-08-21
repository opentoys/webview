//go:build darwin

package darwin

import (
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

func TestDownloadClassCached(t *testing.T) {
	if nsSavePanelClass == 0 {
		t.Fatal("NSSavePanel class not cached")
	}
	if nsWorkspaceClass == 0 {
		t.Fatal("NSWorkspace class not cached")
	}
	if wkDownloadClass == 0 {
		t.Fatal("WKDownload class not cached")
	}
	if setNavigationDelegateSel == 0 {
		t.Fatal("setNavigationDelegate: selector not cached")
	}
	if savePanelSel == 0 {
		t.Fatal("savePanel selector not cached")
	}
}

func TestDownloadDelegateRegistered(t *testing.T) {
	if downloadDelegateClass == 0 {
		t.Fatal("downloadDelegateClass not registered")
	}
	inst := objc.ID(downloadDelegateClass).Send(allocSel)
	inst = inst.Send(initSel)
	if inst == 0 {
		t.Fatal("failed to alloc download delegate instance")
	}
}

func TestDownloadFuncRouting(t *testing.T) {
	p := New()
	called := make(chan string, 1)
	p.DownloadFunc = func(name string, cb func(string)) {
		called <- name
		cb("/tmp/test-download.bin")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	waitWindow(t, p)

	mainThread(func() {
		dl := objc.ID(wkDownloadClass).Send(allocSel)
		dl = dl.Send(initSel)
		p.showSavePanel(dl, nsString("test.bin"), objc.ID(0))
	})
	select {
	case name := <-called:
		if name != "test.bin" {
			t.Fatalf("DownloadFunc got filename %q, want %q", name, "test.bin")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DownloadFunc never called")
	}
	p.Close()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() = %v", err)
	}
}
