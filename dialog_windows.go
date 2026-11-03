//go:build windows

package webview

import (
	"github.com/opentoys/webview/internal/types"
	"github.com/opentoys/webview/internal/windows"
)

func prepareNativeDialogs() error { return windows.PrepareDialogs() }
func nativeDialogOnUI() bool      { return windows.DialogOnUI() }
func cancelNativeDialog()         { windows.CancelDialog() }
func showNativeDialog(kind types.DialogKind, opts FileDialogOptions, stopped func() bool) (string, error) {
	return windows.ShowDialog(kind, opts, stopped)
}
