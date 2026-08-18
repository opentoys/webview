package webview

import (
	"runtime"
	"testing"
	"time"
)

// TestRoundtripDarwin drives the full stack: bootstrap injection, JS call of a
// bound Go func, Go result eval'd back into the webview, and the JS side
// observing it via a second bound func. Darwin only; needs a GUI session.
func TestRoundtripDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	w, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Bind("greet", func(name string) string { return "hi " + name }); err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan string, 1)
	if err := w.Bind("report", func(s string) { resultCh <- s }); err != nil {
		t.Fatal(err)
	}

	runErr := make(chan error, 1)
	go func() { runErr <- w.Run() }()
	// Give the host thread time to create the window and webview. HTML must
	// load after Run(): Navigate is a no-op before the webview exists.
	time.Sleep(300 * time.Millisecond)
	if err := w.SetHTML(`<!doctype html><script>
		window.webviewBridge.call('greet', ['world']).then(r => {
			window.webviewBridge.call('report', [r]);
		});
	</script>`); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-resultCh:
		if r != "hi world" {
			t.Fatalf("roundtrip got %q, want %q", r, "hi world")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("roundtrip timeout: bound func never returned")
	}
	w.Close()
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
}
