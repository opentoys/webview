//go:build darwin

package darwin

import (
	"fmt"
	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/types"
	"sync/atomic"
)

// The panel is owned by the process, never by a Platform or NSWindow.
var standaloneActive atomic.Bool
var standalonePanel objc.ID // AppKit thread only

func DialogOnUI() bool {
	return objc.ID(objc.GetClass("NSThread")).Send(objc.RegisterName("isMainThread")) != 0
}

func PrepareDialogs() error {
	if !hostRunning.Load() && !DialogOnUI() {
		return types.ErrDialogMainThread
	}
	if err := probeUI(); err != nil {
		return fmt.Errorf("%w: %v", types.ErrDialogUnsupported, err)
	}
	hostOnce.Do(initializeHost)
	return nil
}

func ShowDialog(kind types.DialogKind, opts types.FileDialogOptions, stopped func() bool) (string, error) {
	type result struct {
		path string
		err  error
	}
	results := make(chan result, 1)
	mainThread(func() {
		var path string
		var err error
		defer func() { results <- result{path, err} }()
		if stopped() {
			return
		}
		var panel objc.ID
		if kind == types.DialogSave {
			panel = objc.ID(nsSavePanelClass).Send(savePanelSel)
		} else {
			panel = objc.ID(nsOpenPanelClass).Send(openPanelSel)
			panel.Send(setCanChooseFilesSel, kind == types.DialogOpen)
			panel.Send(setCanChooseDirectoriesSel, kind == types.DialogDirectory)
			panel.Send(setAllowsMultipleSelectionSel, false)
		}
		if panel == 0 {
			err = types.ErrDialogUnsupported
			return
		}
		standalonePanel = panel
		standaloneActive.Store(true)
		defer func() { standalonePanel = 0; standaloneActive.Store(false) }()
		applyFileDialogOptions(panel, opts)
		if panel.Send(runModalSel) != 1 {
			err = types.ErrDialogCancelled
			return
		}
		path = goString(panel.Send(panelURLSel).Send(pathSel))
		if path == "" {
			err = fmt.Errorf("webview: dialog returned no filesystem path")
		}
	})
	select {
	case r := <-results:
		return r.path, r.err
	case <-hostDone:
		return "", types.ErrDialogCancelled
	}
}

func CancelDialog() {
	if !hostRunning.Load() && !standaloneActive.Load() {
		return
	}
	select {
	case <-hostReady:
	default:
		return
	}
	mainThread(func() {
		if standalonePanel != 0 {
			standalonePanel.Send(objc.RegisterName("cancel:"), objc.ID(0))
		}
	})
}
