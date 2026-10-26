//go:build windows

package windows

import "fmt"

// setDropTarget is the capability boundary for the WebView2 host. The
// controller must expose ICoreWebView2Controller4 in order to disable its own
// external-drop handling before a Win32 IDropTarget can consume CF_HDROP.
func (p *Platform) setDropTarget(fn func([]string)) error {
	if fn == nil {
		return nil
	}
	return fmt.Errorf("webview: native file drops require WebView2 Controller4 support")
}
