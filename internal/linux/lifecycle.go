//go:build linux

package linux

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/ebitengine/purego"
	"github.com/opentoys/webview/internal/types"
)

var applicationOnce sync.Once
var applicationReady = make(chan struct{})
var applicationDone = make(chan struct{})
var applicationErr error
var applicationTID atomic.Int64
var applicationStop bool
var gtkWindowDestroy func(uintptr)
var closeRequestSignal uintptr

func startApplication() error {
	applicationOnce.Do(func() {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer close(applicationDone)
			applicationErr = ensureUIBindings()
			if applicationErr == nil {
				if !gtkInitCheck4() {
					applicationErr = errNoDisplay
				}
			}
			if applicationErr == nil {
				purego.RegisterLibFunc(&gtkWindowDestroy, gtkLib, "gtk_window_destroy")
				closeRequestSignal = purego.NewCallback(func(window, data uintptr) uintptr {
					if p := lookupPlatform(data); p != nil && p.closeRequest != nil {
						p.closeRequest()
						return 1
					}
					return 0
				})
				applicationTID.Store(int64(syscall.Gettid()))
			}
			close(applicationReady)
			if applicationErr != nil {
				return
			}
			for !applicationStop {
				gMainContextIteration(0, true)
			}
		}()
	})
	<-applicationReady
	return applicationErr
}
func applicationThread(fn func()) {
	select {
	case <-applicationDone:
		return
	default:
	}
	if applicationTID.Load() == int64(syscall.Gettid()) {
		fn()
		return
	}
	done := make(chan struct{})
	dispatchMain(func() { fn(); close(done) })
	select {
	case <-done:
	case <-applicationDone:
	}
}
func StopApplication() error {
	trayBus.stop()
	select {
	case <-applicationReady:
	default:
		return nil
	}
	if applicationErr != nil {
		return nil
	}
	applicationThread(func() { applicationStop = true })
	if applicationTID.Load() != int64(syscall.Gettid()) {
		<-applicationDone
	}
	return nil
}
func (p *gtk) SetCloseRequest(fn func()) { p.closeRequest = fn }
func (p *gtk) Start() error {
	if err := startApplication(); err != nil {
		return err
	}
	var err error
	applicationThread(func() { err = p.setup() })
	return err
}
func (p *gtk) Show() error {
	applicationThread(func() {
		if p.window != 0 {
			gtkWidgetSetVisible(p.window, true)
			gtkWindowPresent(p.window)
		}
	})
	return nil
}
func (p *gtk) Hidden() error {
	applicationThread(func() {
		if p.window != 0 {
			gtkWidgetSetVisible(p.window, false)
		}
	})
	return nil
}

type Tray struct{ tray *linuxTray }

func NewTray(opts types.SystrayOptions) (*Tray, error) {
	t, err := newLinuxTray(opts)
	if err != nil {
		return nil, err
	}
	return &Tray{t}, nil
}
func (t *Tray) Update(opts types.SystrayOptions) error { return t.tray.update(opts) }
func (t *Tray) Close() error                           { return t.tray.close() }

func (p *gtk) SetInitialHidden(hidden bool) { p.initialHidden = hidden }

func WaitApplication() {
	select {
	case <-applicationReady:
		<-applicationDone
	default:
	}
}
