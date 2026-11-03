//go:build linux

package webview

import (
	"github.com/opentoys/webview/internal/linux"
	"github.com/opentoys/webview/internal/types"
)

func prepareNativeDialogs() error { return linux.PrepareDialogs() }
func nativeDialogOnUI() bool      { return linux.DialogOnUI() }
func cancelNativeDialog()         { linux.CancelDialog() }
func showNativeDialog(kind types.DialogKind, opts FileDialogOptions, stopped func() bool) (string, error) {
	return linux.ShowDialog(kind, opts, stopped)
}
