package webview

import (
	"errors"
	"sync"
	"sync/atomic"
)

var (
	ErrClosed             = errors.New("webview: object is closed")
	ErrApplicationStopped = errors.New("webview: application has stopped")
	ErrAlreadyRunning     = errors.New("webview: event loop is already running")
	ErrWindowUnsupported  = errors.New("webview: backend does not support native window lifecycle")
)

type application struct {
	dialogs          *dialogService
	runNative        func(func() error, <-chan struct{}) error
	quitting         atomic.Bool
	makeTray         func(SystrayOptions) (trayBackend, error)
	stopNative       func() error
	registryMu       sync.Mutex
	mu               sync.Mutex
	running, stopped bool
	done             chan struct{}
	windows          map[*W]struct{}
	trays            map[*Systray]struct{}
}

var app *application

func init() { app = newApplication() }

func newApplication() *application {
	return &application{
		dialogs:    newDialogService(),
		runNative:  runNativeApplication,
		makeTray:   newNativeTray,
		stopNative: stopNativeApplication,
		done:       make(chan struct{}),
		windows:    make(map[*W]struct{}),
		trays:      make(map[*Systray]struct{}),
	}
}

// Run owns the process lifetime. Closing the last window or tray does not stop it.
// The event loop may only be run once in a process.
func Run() error {
	app.mu.Lock()
	if app.stopped {
		app.mu.Unlock()
		return ErrApplicationStopped
	}
	if app.running {
		app.mu.Unlock()
		return ErrAlreadyRunning
	}
	app.running = true
	done := app.done
	err := app.runNative(func() error {
		defer app.mu.Unlock()
		for _, w := range app.windowSnapshot() {
			if err := w.start(); err != nil {
				return err
			}
		}
		for _, s := range app.traySnapshot() {
			if err := s.start(); err != nil {
				return err
			}
		}
		return nil
	}, done)

	if err != nil {
		_ = Quit()
		return err
	}
	<-done
	return nil
}

// Quit destroys all native objects and stops the application, without waiting
// for user callbacks and without terminating the Go process.
func Quit() error {
	app.mu.Lock()
	if app.stopped {
		app.mu.Unlock()
		return nil
	}
	app.stopped = true
	app.quitting.Store(true)
	windows, trays := app.windowSnapshot(), app.traySnapshot()
	app.mu.Unlock()
	app.dialogs.stop()
	var result error
	for _, w := range windows {
		result = errors.Join(result, w.Close())
	}
	for _, s := range trays {
		result = errors.Join(result, s.Close())
	}
	result = errors.Join(result, app.stopNative())
	close(app.done)
	return result
}

type nativeWindow interface {
	Start() error
	Show() error
	Hidden() error
	SetCloseRequest(func())
}

func (w *W) start() error {
	w.lifeMu.Lock()
	defer w.lifeMu.Unlock()
	if w.closed || w.started {
		return nil
	}
	if p, ok := w.p.(nativeWindow); ok {
		p.SetCloseRequest(func() { go w.requestClose() })
		if initial, ok := w.p.(interface{ SetInitialHidden(bool) }); ok {
			initial.SetInitialHidden(w.hidden)
		}
		if err := p.Start(); err != nil {
			return err
		}
		w.started = true
		if w.hidden {
			return p.Hidden()
		}
		return nil
	}
	if p, ok := w.p.(interface{ Start() error }); ok {
		if err := p.Start(); err != nil {
			return err
		}
		w.started = true
		return nil
	}
	return ErrWindowUnsupported
}
func (w *W) windowVisibility(hidden bool) error {
	w.lifeMu.Lock()
	if w.closed {
		w.lifeMu.Unlock()
		return ErrClosed
	}
	p, ok := w.p.(nativeWindow)
	if !ok {
		w.lifeMu.Unlock()
		return ErrWindowUnsupported
	}
	w.hidden = hidden
	started := w.started
	w.lifeMu.Unlock()
	if !started {
		return nil
	}
	if hidden {
		return p.Hidden()
	}
	return p.Show()
}

// Show displays and activates this window. A closed window cannot be reopened.
func (w *W) Show() error { return w.windowVisibility(false) }

// Hidden hides this window while retaining its page and JavaScript state.
func (w *W) Hidden() error { return w.windowVisibility(true) }

// OnClose intercepts native close requests. nil restores destruction. Close
// and Quit always bypass this callback. Repeated requests coalesce while it runs.
func (w *W) OnClose(fn func()) error {
	w.lifeMu.Lock()
	defer w.lifeMu.Unlock()
	if w.closed {
		return ErrClosed
	}
	if _, ok := w.p.(nativeWindow); !ok {
		return ErrWindowUnsupported
	}
	w.onClose = fn
	return nil
}
func (w *W) requestClose() {
	w.lifeMu.Lock()
	if w.closed || app.quitting.Load() || w.closePending {
		w.lifeMu.Unlock()
		return
	}
	fn := w.onClose
	if fn == nil {
		w.lifeMu.Unlock()
		go w.Close()
		return
	}
	w.closePending = true
	w.lifeMu.Unlock()
	go func() {
		defer func() {
			w.lifeMu.Lock()
			w.closePending = false
			w.lifeMu.Unlock()
		}()
		w.lifeMu.Lock()
		valid := !w.closed && !app.quitting.Load()
		w.lifeMu.Unlock()
		if valid {
			fn()
		}
	}()
}

// Pending configuration is serialized with creation. Live operations are
// serialized by the native UI queue, without holding a Go lock while waiting
// for a page or a modal operation; Quit must be able to destroy that window.
func (w *W) windowCall(fn func() error) error {
	w.lifeMu.Lock()
	if w.closed {
		w.lifeMu.Unlock()
		return ErrClosed
	}
	if !w.started {
		defer w.lifeMu.Unlock()
		return fn()
	}
	w.lifeMu.Unlock()
	var err error
	w.p.MainThread(func() {
		w.lifeMu.Lock()
		closed := w.closed
		w.lifeMu.Unlock()
		if closed {
			err = ErrClosed
			return
		}
		err = fn()
	})
	return err
}

func (a *application) windowSnapshot() []*W {
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	out := make([]*W, 0, len(a.windows))
	for w := range a.windows {
		out = append(out, w)
	}
	return out
}
func (a *application) traySnapshot() []*Systray {
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	out := make([]*Systray, 0, len(a.trays))
	for s := range a.trays {
		out = append(out, s)
	}
	return out
}
