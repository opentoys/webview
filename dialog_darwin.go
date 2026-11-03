//go:build darwin

package webview

import (
	"github.com/opentoys/webview/internal/darwin"
	"github.com/opentoys/webview/internal/types"
)

func prepareNativeDialogs() error { return darwin.PrepareDialogs() }
func nativeDialogOnUI() bool      { return darwin.DialogOnUI() }
func cancelNativeDialog()         { darwin.CancelDialog() }
func showNativeDialog(kind types.DialogKind, opts FileDialogOptions, stopped func() bool) (string, error) {
	return darwin.ShowDialog(kind, opts, stopped)
}
