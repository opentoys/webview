package webview

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"runtime"
	"sync"

	"github.com/opentoys/webview/internal/types"
)

type trayBackend interface {
	Update(SystrayOptions) error
	Close() error
}

// Systray is an independent native tray item, unrelated to any WebView.
type Systray struct {
	mu      sync.Mutex
	opts    SystrayOptions
	p       trayBackend
	closed  bool
	actions types.MenuActions
}

func traySnapshot(opts SystrayOptions) (SystrayOptions, error) {
	if err := validateMenuItems(opts.Items); err != nil {
		return opts, err
	}
	if len(opts.Icon) == 0 {
		if opts.Title == "" {
			return opts, errors.New("webview: systray requires an icon or title")
		}
		if runtime.GOOS != "darwin" {
			return opts, ErrMenuUnsupported
		}
	} else if _, err := png.Decode(bytes.NewReader(opts.Icon)); err != nil {
		return opts, fmt.Errorf("webview: systray PNG: %w", err)
	}
	opts.Icon = append([]byte(nil), opts.Icon...)
	opts.Items = append([]MenuItem(nil), opts.Items...)
	return opts, nil
}
func NewSystray(opts SystrayOptions) (*Systray, error) {
	opts, err := traySnapshot(opts)
	if err != nil {
		return nil, err
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.stopped {
		return nil, ErrApplicationStopped
	}
	s := &Systray{opts: opts}
	if app.running {
		if err := s.start(); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	app.registryMu.Lock()
	app.trays[s] = struct{}{}
	app.registryMu.Unlock()
	return s, nil
}
func (s *Systray) callbacks(opts SystrayOptions) SystrayOptions {
	// Both menu and primary clicks share a generation, invalidated on update/close.
	all := append(types.NormalizeMenuItems(opts.Items), MenuItem{Action: opts.OnClick})
	for i := range all {
		if fn := all[i].Action; fn != nil {
			all[i].Action = func() {
				if !app.quitting.Load() {
					fn()
				}
			}
		}
	}
	all = s.actions.Replace(all)
	// The synthetic click item is never a separator and survives normalization.
	opts.OnClick = all[len(all)-1].Action
	opts.Items = all[:len(all)-1]
	return opts
}
func (s *Systray) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.p != nil {
		return nil
	}
	p, err := app.makeTray(s.callbacks(s.opts))
	if err != nil {
		s.actions.Close()
		return err
	}
	s.p = p
	return nil
}
func (s *Systray) Update(opts SystrayOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	opts, err := traySnapshot(opts)
	if err != nil {
		return err
	}
	if s.p != nil {
		if err := s.p.Update(s.callbacks(opts)); err != nil {
			return err
		}
	}
	s.opts = opts
	return nil
}
func (s *Systray) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	defer func() { app.registryMu.Lock(); delete(app.trays, s); app.registryMu.Unlock() }()
	s.actions.Close()
	if s.p != nil {
		return s.p.Close()
	}
	return nil
}
