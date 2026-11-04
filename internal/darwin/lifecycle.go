//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/types"
)

// Accessed only on the AppKit host thread. Every delegate and view routes to
// its owning window, never to the last-created WebView.
var objectPlatforms = map[objc.ID]*Platform{}

func platformFor(id objc.ID) *Platform        { return objectPlatforms[id] }
func (p *Platform) SetCloseRequest(fn func()) { p.closeRequest = fn }
func (p *Platform) Start() error {
	startAppHost()
	var err error
	mainThread(func() { err = p.setup() })
	if err != nil {
		_ = p.Close()
	}
	return err
}
func (p *Platform) Show() error {
	mainThread(func() {
		if p.window != 0 {
			p.window.Send(makeKeyAndOrderFrontSel, 0)
			objc.ID(nsAppClass).Send(sharedApplicationSel).Send(activateIgnoringOtherAppsSel, true)
		}
	})
	return nil
}
func (p *Platform) Hidden() error {
	mainThread(func() {
		if p.window != 0 {
			p.window.Send(orderOutSel, 0)
		}
	})
	return nil
}
func StopApplication() error {
	select {
	case <-hostReady:
	default:
		return nil
	}
	if !hostRunning.Load() {
		hostDoneOnce.Do(func() { close(hostDone) })
		return nil
	}
	mainThread(func() {
		app := objc.ID(nsAppClass).Send(sharedApplicationSel)
		app.Send(objc.RegisterName("stop:"), objc.ID(0))
		event := objc.ID(objc.GetClass("NSEvent")).Send(
			objc.RegisterName("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			uintptr(15), struct{ X, Y float64 }{}, uintptr(0), float64(0), int64(0), objc.ID(0), int16(0), int64(0), int64(0),
		)
		app.Send(objc.RegisterName("postEvent:atStart:"), event, true)
	})
	if objc.ID(nsThreadClass).Send(currentThreadSel) != hostThread {
		<-hostDone
	}
	return nil
}

// Tray owns only status-bar resources. No NSWindow or WKWebView is created.
type Tray struct{ owner *Platform }

func NewTray(opts types.SystrayOptions) (*Tray, error) {
	probe()
	if probeErr != nil {
		return nil, probeErr
	}
	startAppHost()
	p := &Platform{trayOptions: &opts}
	var err error
	mainThread(func() { err = p.applySystray() })
	if err != nil {
		mainThread(p.removeSystray)
		return nil, err
	}
	return &Tray{p}, nil
}
func (t *Tray) Update(opts types.SystrayOptions) error {
	var err error
	mainThread(func() {
		t.owner.trayOptions = &opts
		err = t.owner.applySystray()
	})
	return err
}
func (t *Tray) Close() error {
	mainThread(t.owner.removeSystray)
	return nil
}

// primary tray button actions have a separate target from menu-item actions.
var trayButtons = map[objc.ID]*Platform{}
var trayButtonClass objc.Class

func configureTrayClick(p *Platform, button objc.ID, opts *types.SystrayOptions) error {
	if trayButtonClass == 0 {
		var err error
		trayButtonClass, err = objc.RegisterClass("GoWebviewTrayButton", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: objc.RegisterName("clicked:"), Fn: func(id objc.ID, cmd objc.SEL, sender objc.ID) {
				owner := trayButtons[sender]
				if owner == nil || owner.trayOptions == nil {
					return
				}
				event := objc.ID(nsAppClass).Send(sharedApplicationSel).Send(objc.RegisterName("currentEvent"))
				if event.Send(objc.RegisterName("type")) == 4 || owner.trayOptions.OnClick == nil {
					owner.statusItem.Send(objc.RegisterName("popUpStatusItemMenu:"), owner.trayNative.menu)
				} else {
					owner.trayOptions.OnClick()
				}
			}},
		})
		if err != nil {
			return err
		}
	}
	if p.trayTarget == 0 {
		p.trayTarget = objc.ID(trayButtonClass).Send(allocSel).Send(initSel)
	}
	trayButtons[button] = p
	button.Send(setTargetSel, p.trayTarget)
	button.Send(objc.RegisterName("setAction:"), objc.RegisterName("clicked:"))
	button.Send(objc.RegisterName("sendActionOn:"), uintptr((1<<2)|(1<<4)))
	return nil
}

func (p *Platform) SetInitialHidden(hidden bool) { p.initialHidden = hidden }
