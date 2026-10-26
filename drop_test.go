package webview

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type dropPlatformStub struct {
	dialogPlatformStub
	fn  func([]string)
	err error
}

func (p *dropPlatformStub) SetFilesDropped(fn func([]string)) error {
	if p.err != nil && fn != nil {
		return p.err
	}
	p.fn = fn
	return nil
}

func TestOnFilesDroppedRegistrationAndSnapshot(t *testing.T) {
	p := &dropPlatformStub{}
	w := &W{p: p}
	got := make(chan []string, 1)
	if err := w.OnFilesDropped(func(paths []string) { got <- paths }); err != nil {
		t.Fatal(err)
	}
	if p.fn == nil {
		t.Fatal("platform callback was not installed")
	}
	paths := []string{"/tmp/a", "/tmp/../tmp/b"}
	p.fn(paths)
	paths[0] = "/changed"
	select {
	case got := <-got:
		if len(got) != 2 || got[0] != "/tmp/a" || got[1] != "/tmp/b" {
			t.Fatalf("paths = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("callback was not delivered")
	}
	if err := w.OnFilesDropped(nil); err != nil {
		t.Fatal(err)
	}
	if p.fn != nil {
		t.Fatal("platform callback was not removed")
	}
}

func TestOnFilesDroppedSerialAndClose(t *testing.T) {
	p := &dropPlatformStub{}
	w := &W{p: p}
	var mu sync.Mutex
	var got []string
	var active, maxActive int
	done := make(chan struct{})
	if err := w.OnFilesDropped(func(paths []string) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		got = append(got, paths[0])
		active--
		n := len(got)
		mu.Unlock()
		if n == 3 {
			close(done)
		}
	}); err != nil {
		t.Fatal(err)
	}
	p.fn([]string{"/tmp/1"})
	p.fn([]string{"/tmp/2"})
	p.fn([]string{"/tmp/3"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("callbacks did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if maxActive != 1 || len(got) != 3 || got[0] != "/tmp/1" || got[2] != "/tmp/3" {
		t.Fatalf("got=%v max=%d", got, maxActive)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.OnFilesDropped(func([]string) {}); err == nil {
		t.Fatal("registration after Close succeeded")
	}
}

func TestOnFilesDroppedForwardsErrors(t *testing.T) {
	p := &dropPlatformStub{err: errors.New("install failed")}
	w := &W{p: p}
	if err := w.OnFilesDropped(func([]string) {}); !errors.Is(err, p.err) {
		t.Fatalf("error = %v", err)
	}
	if w.dropFn != nil {
		t.Fatal("handler changed after failed install")
	}
}
