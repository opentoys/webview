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
