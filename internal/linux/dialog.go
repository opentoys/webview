//go:build linux

package linux

import (
	"fmt"
	"github.com/opentoys/webview/internal/types"
	"syscall"
)

var standaloneChooser uintptr // GTK thread only
var standaloneResponse *dialogResp

func PrepareDialogs() error {
	if err := startApplication(); err != nil {
		return fmt.Errorf("%w: %v", types.ErrDialogUnsupported, err)
	}
	return nil
}
func DialogOnUI() bool {
	return applicationTID.Load() != 0 && applicationTID.Load() == int64(syscall.Gettid())
}

func ShowDialog(kind types.DialogKind, opts types.FileDialogOptions, stopped func() bool) (string, error) {
	type result struct {
		path string
		err  error
	}
	results := make(chan result, 1)
	applicationThread(func() {
		var path string
		var err error
		defer func() { results <- result{path, err} }()
		if stopped() {
			return
		}
		action, accept := gtkFileChooserActionOpen, "_Open"
		if kind == types.DialogDirectory {
			action = gtkFileChooserActionSelectFolder
		}
		if kind == types.DialogSave {
			action, accept = gtkFileChooserActionSave, "_Save"
		}
		dlg := gtkFileChooserNativeNew(opts.Title, 0, action, accept, "_Cancel")
		if dlg == 0 {
			err = types.ErrDialogUnsupported
			return
		}
		defer gObjectUnref(dlg)
		gtkFileChooserSetSelectMultiple(dlg, false)
		applyFileDialogOptions(dlg, opts)
		if kind == types.DialogSave && opts.Filename != "" {
			gtkFileChooserSetCurrentName(dlg, opts.Filename)
		}
		standaloneChooser = dlg
		defer func() { standaloneChooser = 0; standaloneResponse = nil }()
		if runFallbackDialog(dlg) != gtkResponseAccept {
			err = types.ErrDialogCancelled
			return
		}
		file := gtkFileChooserGetFile(dlg)
		if file == 0 {
			err = fmt.Errorf("webview: dialog returned no filesystem path")
			return
		}
		defer gObjectUnref(file)
		raw := gFileGetPath(file)
		if raw != 0 {
			path = cstr(raw)
			gFree(raw)
		}
		if path == "" {
			err = fmt.Errorf("webview: dialog returned no filesystem path")
		}
	})
	select {
	case r := <-results:
		return r.path, r.err
	case <-applicationDone:
		return "", types.ErrDialogCancelled
	}
}
func CancelDialog() {
	select {
	case <-applicationReady:
	default:
		return
	}
	if applicationErr != nil {
		return
	}
	applicationThread(func() {
		if standaloneChooser != 0 {
			gtkNativeDialogHide(standaloneChooser)
			dialogRespMu.Lock()
			if standaloneResponse != nil {
				standaloneResponse.done = true
			}
			dialogRespMu.Unlock()
		}
	})
}
