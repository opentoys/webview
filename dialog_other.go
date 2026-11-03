//go:build !darwin && !windows && !linux

package webview

import "github.com/opentoys/webview/internal/types"

func prepareNativeDialogs() error { return ErrDialogUnsupported }
func nativeDialogOnUI() bool      { return false }
func cancelNativeDialog()         {}
func showNativeDialog(types.DialogKind, FileDialogOptions, func() bool) (string, error) {
	return "", ErrDialogUnsupported
}
