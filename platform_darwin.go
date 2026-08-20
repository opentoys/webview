//go:build darwin

package webview

import "github.com/opentoys/webview/platform/darwin"

// Type aliases forward the platform package's types so callers don't need
// to import platform/darwin directly.
type DialogKind = darwin.DialogKind

const (
	DialogAlert   = darwin.DialogAlert
	DialogConfirm = darwin.DialogConfirm
	DialogPrompt  = darwin.DialogPrompt
)

type SizeHint = darwin.SizeHint

const (
	SizeNone  = darwin.SizeNone
	SizeMin   = darwin.SizeMin
	SizeMax   = darwin.SizeMax
	SizeFixed = darwin.SizeFixed
)

// OpenPanelParams describes the <input type=file> that triggered the picker.
type OpenPanelParams struct {
	AllowsMultipleSelection bool
	AllowsDirectories       bool
}

// OpenPanelFunc replaces the native file picker for <input type=file>.
type OpenPanelFunc func(params OpenPanelParams, callback func(paths []string, ok bool))

// DownloadFunc replaces the native save panel for file downloads.
type DownloadFunc func(suggestedFilename string, callback func(savePath string))

// W is the top-level webview handle. Darwin includes handler fields for
// dialog, file-panel, and download overrides.
type W struct {
	p         Platform
	bridge    *bridge
	dialog    func(kind DialogKind, message, defaultInput string) (string, bool)
	openPanel OpenPanelFunc
	openPanelSet func(OpenPanelFunc)
	download     DownloadFunc
	downloadSet  func(DownloadFunc)
}

// SetDialogHandler overrides the default JS dialog handler.
func (w *W) SetDialogHandler(h func(kind DialogKind, message, defaultInput string) (string, bool)) {
	w.dialog = h
}

// SetOpenPanelHandler replaces the native file picker for <input type=file>.
func (w *W) SetOpenPanelHandler(h OpenPanelFunc) {
	w.openPanel = h
	if w.openPanelSet != nil {
		w.openPanelSet(h)
	}
}

// SetDownloadHandler replaces the native save panel for file downloads.
func (w *W) SetDownloadHandler(h DownloadFunc) {
	w.download = h
	if w.downloadSet != nil {
		w.downloadSet(h)
	}
}

func buildPlatform(opts Options, w *W) Platform {
	w.dialog = func(kind DialogKind, message, defaultInput string) (string, bool) {
		switch kind {
		case DialogConfirm:
			return "", false
		default:
			return defaultInput, true
		}
	}
	p := darwin.New()
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	p.DialogFunc = func(kind DialogKind, message, defaultInput string) (string, bool) {
		if w.dialog != nil {
			return w.dialog(kind, message, defaultInput)
		}
		switch kind {
		case DialogConfirm:
			return "", false
		default:
			return defaultInput, true
		}
	}
	w.openPanelSet = func(fn OpenPanelFunc) {
		if fn != nil {
			p.OpenPanelFunc = func(params darwin.OpenPanelParams, cb func([]string, bool)) {
				fn(OpenPanelParams{
					AllowsMultipleSelection: params.AllowsMultipleSelection,
					AllowsDirectories:       params.AllowsDirectories,
				}, cb)
			}
		} else {
			p.OpenPanelFunc = nil
		}
	}
	w.downloadSet = func(fn DownloadFunc) {
		if fn != nil {
			p.DownloadFunc = func(suggestedFilename string, cb func(string)) {
				fn(suggestedFilename, cb)
			}
		} else {
			p.DownloadFunc = nil
		}
	}
	return p
}
