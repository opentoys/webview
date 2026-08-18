package darwin

import (
	"testing"
	"time"
)

func TestWindowOpens(t *testing.T) {
	p := New()
	done := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.Close()
		done <- struct{}{}
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	<-done
}

func TestTitleAndHTML(t *testing.T) {
	p := New()
	p.SetTitle("hello")
	p.SetHTML("<html><body><h1 id='t'>old</h1></body></html>")
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.Close()
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
}

func TestEval(t *testing.T) {
	p := New()
	p.SetHTML("<html><body><h1 id='t'>old</h1></body></html>")
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.Close()
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
}
