package webview

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opentoys/webview/internal/chrome"
)

type lifecyclePlatform struct {
	dialogPlatformStub
	starts, closes, shows, hides atomic.Int32
	initialHidden                bool
	startErr                     error
	request                      func()
}

func (p *lifecyclePlatform) Start() error {
	p.starts.Add(1)
	return p.startErr
}
func (p *lifecyclePlatform) Close() error {
	p.closes.Add(1)
	return nil
}
func (p *lifecyclePlatform) Show() error {
	p.shows.Add(1)
	return nil
}
func (p *lifecyclePlatform) Hidden() error {
	p.hides.Add(1)
	return nil
}
func (p *lifecyclePlatform) SetCloseRequest(fn func())    { p.request = fn }
func (p *lifecyclePlatform) SetInitialHidden(hidden bool) { p.initialHidden = hidden }

type lifecycleTray struct {
	opts   SystrayOptions
	closes atomic.Int32
}

func (p *lifecycleTray) Update(opts SystrayOptions) error {
	p.opts = opts
	return nil
}
func (p *lifecycleTray) Close() error {
	p.closes.Add(1)
	return nil
}

func isolatedApplication(t *testing.T) {
	t.Helper()
	previous := app
	app = newApplication()
	app.runNative = func(start func() error, done <-chan struct{}) error {
		if err := start(); err != nil {
			return err
		}
		<-done
		return nil
	}
	app.stopNative = func() error { return nil }
	app.makeTray = func(opts SystrayOptions) (trayBackend, error) { return &lifecycleTray{opts: opts}, nil }
	t.Cleanup(func() {
		_ = Quit()
		app = previous
	})
}
func pendingWindow(p *lifecyclePlatform) *W {
	w := &W{p: p, bridge: newBridge()}
	app.registryMu.Lock()
	app.windows[w] = struct{}{}
	app.registryMu.Unlock()
	return w
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for !fn() {
		select {
		case <-deadline:
			t.Fatal("timed out")
		case <-time.After(time.Millisecond):
		}
	}
}
func running(t *testing.T) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- Run() }()
	waitFor(t, func() bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return app.running
	})
	return result
}
func stillRunning(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("Run returned before Quit: %v", err)
	default:
	}
}
func TestApplicationLifecycle(t *testing.T) {
	isolatedApplication(t)
	p1, p2 := &lifecyclePlatform{}, &lifecyclePlatform{}
	w1, w2 := pendingWindow(p1), pendingWindow(p2)
	if err := w1.Hidden(); err != nil {
		t.Fatal(err)
	}
	tray1, err := NewSystray(SystrayOptions{Icon: menuTestIcon(t)})
	if err != nil {
		t.Fatal(err)
	}
	tray2, err := NewSystray(SystrayOptions{Icon: menuTestIcon(t)})
	if err != nil {
		t.Fatal(err)
	}
	result := running(t)
	if err := Run(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatal(err)
	}
	if p1.starts.Load() != 1 || p2.starts.Load() != 1 || !p1.initialHidden {
		t.Fatal("initial windows not created as configured")
	}
	for range 3 {
		if err := w1.Show(); err != nil {
			t.Fatal(err)
		}
		if err := w1.Hidden(); err != nil {
			t.Fatal(err)
		}
	}
	if p1.starts.Load() != 1 {
		t.Fatal("visibility recreated WebView")
	}
	if err := w1.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w1.Close(); err != nil {
		t.Fatal(err)
	}
	if p1.closes.Load() != 1 || p2.closes.Load() != 0 {
		t.Fatal("Close crossed object boundaries")
	}
	for _, err := range []error{w1.Show(), w1.Hidden(), w1.Init("x"), w1.SetHTML("x"), w1.SetTitle("x"), w1.OnClose(nil)} {
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("closed operation: %v", err)
		}
	}
	if err := w2.Close(); err != nil {
		t.Fatal(err)
	}
	stillRunning(t, result)
	// A tray's callbacks remain valid after every window has been destroyed.
	clicked := make(chan struct{}, 1)
	if err := tray2.Update(SystrayOptions{Icon: menuTestIcon(t), OnClick: func() { clicked <- struct{}{} }}); err != nil {
		t.Fatal(err)
	}
	tray2.p.(*lifecycleTray).opts.OnClick()
	select {
	case <-clicked:
	case <-time.After(time.Second):
		t.Fatal("tray lost callback")
	}
	if err := tray1.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tray2.Close(); err != nil {
		t.Fatal(err)
	}
	stillRunning(t, result)
	if err := Quit(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := Quit(); err != nil {
		t.Fatal(err)
	}
	if err := Run(); !errors.Is(err, ErrApplicationStopped) {
		t.Fatal(err)
	}
	if _, err := New(Options{}); !errors.Is(err, ErrApplicationStopped) {
		t.Fatal(err)
	}
	if _, err := NewSystray(SystrayOptions{Icon: menuTestIcon(t)}); !errors.Is(err, ErrApplicationStopped) {
		t.Fatal(err)
	}
}
func TestApplicationEmptyAndStartupFailure(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		isolatedApplication(t)
		result := running(t)
		stillRunning(t, result)
		_ = Quit()
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("startup failure", func(t *testing.T) {
		isolatedApplication(t)
		sentinel := errors.New("native setup failed")
		p := &lifecyclePlatform{startErr: sentinel}
		pendingWindow(p)
		if err := Run(); !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
		if p.closes.Load() != 1 {
			t.Fatal("failed startup leaked window")
		}
		if err := Run(); !errors.Is(err, ErrApplicationStopped) {
			t.Fatal(err)
		}
	})
	t.Run("pending close", func(t *testing.T) {
		isolatedApplication(t)
		p := &lifecyclePlatform{}
		w := pendingWindow(p)
		_ = w.Close()
		tray, err := NewSystray(SystrayOptions{Icon: menuTestIcon(t)})
		if err != nil {
			t.Fatal(err)
		}
		_ = tray.Close()
		result := running(t)
		if p.starts.Load() != 0 {
			t.Fatal("closed object initialized")
		}
		_ = Quit()
		<-result
	})
}
func TestWindowCloseCallback(t *testing.T) {
	isolatedApplication(t)
	p := &lifecyclePlatform{}
	w := pendingWindow(p)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	if err := w.OnClose(func() {
		calls.Add(1)
		close(entered)
		<-release
		_ = w.Hidden()
		close(finished)
	}); err != nil {
		t.Fatal(err)
	}
	w.requestClose()
	<-entered
	for range 20 {
		w.requestClose()
	}
	if calls.Load() != 1 {
		t.Fatal("close requests not coalesced")
	}
	close(release)
	<-finished
	waitFor(t, func() bool {
		w.lifeMu.Lock()
		defer w.lifeMu.Unlock()
		return !w.closePending
	})
	if err := w.OnClose(func() { _ = w.Close() }); err != nil {
		t.Fatal(err)
	}
	w.requestClose()
	waitFor(t, func() bool { return p.closes.Load() == 1 })
	p2 := &lifecyclePlatform{}
	w2 := pendingWindow(p2)
	if err := w2.OnClose(func() { t.Error("programmatic close invoked callback") }); err != nil {
		t.Fatal(err)
	}
	_ = w2.Close()
	p3 := &lifecyclePlatform{}
	w3 := pendingWindow(p3)
	if err := w3.OnClose(nil); err != nil {
		t.Fatal(err)
	}
	w3.requestClose()
	waitFor(t, func() bool { return p3.closes.Load() == 1 })
}
func TestQuitInsideCallbackDoesNotWaitForCallbacks(t *testing.T) {
	isolatedApplication(t)
	w := pendingWindow(&lifecyclePlatform{})
	release, finished := make(chan struct{}), make(chan struct{})
	_ = w.OnClose(func() {
		_ = Quit()
		close(finished)
		<-release
	})
	result := running(t)
	w.requestClose()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Quit deadlocked in OnClose")
	}
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("Run waited for user callback")
	}
	close(release)
	waitFor(t, func() bool {
		w.lifeMu.Lock()
		defer w.lifeMu.Unlock()
		return !w.closePending
	})
}
func TestChromeWindowLifecycleUnsupported(t *testing.T) {
	w := &W{p: chrome.New(chrome.Options{})}
	for _, err := range []error{w.Show(), w.Hidden(), w.OnClose(nil)} {
		if !errors.Is(err, ErrWindowUnsupported) {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	if err := w.Show(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

// A running Eval may await a promise (Chrome). Closing the window must be
// able to interrupt it rather than waiting behind the callback's Go lock.
type waitingPlatform struct {
	lifecyclePlatform
	entered, release chan struct{}
}

func (p *waitingPlatform) Eval(string) error {
	close(p.entered)
	<-p.release
	return nil
}
func (p *waitingPlatform) Close() error {
	close(p.release)
	return nil
}
func TestQuitInterruptsWaitingWindowOperation(t *testing.T) {
	isolatedApplication(t)
	p := &waitingPlatform{entered: make(chan struct{}), release: make(chan struct{})}
	w := &W{p: p, bridge: newBridge()}
	app.windows[w] = struct{}{}
	result := running(t)
	evaluated := make(chan struct{})
	go func() {
		_ = w.Eval("pending promise")
		close(evaluated)
	}()
	<-p.entered
	quit := make(chan error, 1)
	go func() { quit <- Quit() }()
	select {
	case err := <-quit:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Quit waited for Eval callback")
	}
	<-evaluated
	<-result
}
