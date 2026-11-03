package webview

import (
	"path/filepath"
	"sync"

	"github.com/opentoys/webview/internal/types"
)

var (
	ErrDialogMainThread = types.ErrDialogMainThread
	ErrDialogReentrant  = types.ErrDialogReentrant
)

// ChooseDir synchronously selects one directory, without creating a WebView.
// Before Run on macOS, call from the main goroutine.
func ChooseDir() (string, error) {
	return app.dialogs.choose(types.DialogDirectory, FileDialogOptions{})
}

// ChooseFile synchronously selects one file. Requests are displayed in order.
// Before Run on macOS, call from the main goroutine.
func ChooseFile(filters ...FileFilter) (string, error) {
	return app.dialogs.choose(types.DialogOpen, FileDialogOptions{Filters: filters})
}

// SaveFile selects an absolute path with native overwrite confirmation. It does
// not create a file. Before Run on macOS, call from the main goroutine.
func SaveFile(opts FileDialogOptions) (string, error) {
	return app.dialogs.choose(types.DialogSave, opts)
}

type dialogService struct {
	mu       sync.Mutex
	stopped  chan struct{}
	tail     chan struct{}
	requests map[chan struct{}]struct{}
	prepare  func() error
	onUI     func() bool
	show     func(types.DialogKind, FileDialogOptions, func() bool) (string, error)
	cancel   func()
}

func newDialogService() *dialogService {
	return &dialogService{stopped: make(chan struct{}), prepare: prepareNativeDialogs,
		onUI: nativeDialogOnUI, show: showNativeDialog, cancel: cancelNativeDialog}
}

func snapshotDialogOptions(opts FileDialogOptions) FileDialogOptions {
	filters := make([]FileFilter, len(opts.Filters))
	for i, f := range opts.Filters {
		filters[i] = f
		filters[i].Extensions = append([]string(nil), f.Extensions...)
		filters[i].MIMETypes = append([]string(nil), f.MIMETypes...)
	}
	opts.Filters = filters
	return opts
}

func (d *dialogService) isStopped() bool {
	select {
	case <-d.stopped:
		return true
	default:
		return false
	}
}

func (d *dialogService) choose(kind types.DialogKind, opts FileDialogOptions) (string, error) {
	opts = snapshotDialogOptions(opts)
	d.mu.Lock()
	if d.isStopped() {
		d.mu.Unlock()
		return "", ErrApplicationStopped
	}
	previous := d.tail
	if previous != nil && d.onUI() {
		d.mu.Unlock()
		return "", ErrDialogReentrant
	}
	finished := make(chan struct{})
	d.tail = finished
	if d.requests == nil {
		d.requests = make(map[chan struct{}]struct{})
	}
	d.requests[finished] = struct{}{}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		close(finished)
		delete(d.requests, finished)
		if d.tail == finished {
			d.tail = nil
		}
		d.mu.Unlock()
	}()
	if previous != nil {
		select {
		case <-previous:
		case <-d.stopped:
			return "", ErrApplicationStopped
		}
	}
	if d.isStopped() {
		return "", ErrApplicationStopped
	}
	if err := d.prepare(); err != nil {
		if d.isStopped() {
			return "", ErrApplicationStopped
		}
		return "", err
	}
	if d.isStopped() {
		return "", ErrApplicationStopped
	}
	path, err := d.show(kind, opts, d.isStopped)
	if d.isStopped() {
		return "", ErrApplicationStopped
	}
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", ErrDialogCancelled
	}
	return filepath.Abs(filepath.Clean(path))
}

func (d *dialogService) stop() {
	d.mu.Lock()
	if d.isStopped() {
		d.mu.Unlock()
		return
	}
	close(d.stopped)
	pending := make([]chan struct{}, 0, len(d.requests))
	for done := range d.requests {
		pending = append(pending, done)
	}
	d.mu.Unlock()
	d.cancel()
	// A Quit callback inside a native modal loop must be allowed to unwind it.
	if !d.onUI() {
		for _, done := range pending {
			<-done
		}
	}
}
