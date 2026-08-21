//go:build windows

package windows

import (
	"testing"
	"time"
)

func TestNewPlatform(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
}

func TestWindowOpensAndCloses(t *testing.T) {
	p := New()
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()

	time.AfterFunc(500*time.Millisecond, func() {
		p.Close()
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after Close()")
	}
}

func TestTitleBeforeRun(t *testing.T) {
	p := New()
	p.SetTitle("Test Title")
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	time.AfterFunc(500*time.Millisecond, func() { p.Close() })
	<-errCh
}

func TestHTMLBeforeRun(t *testing.T) {
	p := New()
	p.SetHTML("<h1>Hello</h1>")
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	time.AfterFunc(500*time.Millisecond, func() { p.Close() })
	<-errCh
}

func TestNavigateBeforeRun(t *testing.T) {
	p := New()
	p.Navigate("https://example.com")
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()
	time.AfterFunc(1*time.Second, func() { p.Close() })
	<-errCh
}

func TestEvalAfterRun(t *testing.T) {
	p := New()
	p.SetHTML("<html><body></body></html>")
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()

	time.AfterFunc(1*time.Second, func() {
		err := p.Eval("document.title = 'evaluated'")
		if err != nil {
			t.Errorf("Eval() = %v", err)
		}
		p.Close()
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}
