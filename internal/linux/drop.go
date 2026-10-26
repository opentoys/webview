//go:build linux

package linux

import "fmt"

// setDropTarget is kept separate from the generic GTK lifecycle so builds on
// older GTK runtimes fail at the precise capability boundary.  WebKitGTK's
// supported minimum in this package predates GdkFileList (GTK 4.6), so a
// runtime without that type cannot safely distinguish local files from URI or
// virtual drops.
//
// The dynamic GTK loader currently has no GdkFileList bindings. Keeping this
// check explicit prevents silently falling back to web-page drops.
func (p *gtk) setDropTarget(fn func([]string)) error {
	if fn == nil {
		return nil
	}
	return fmt.Errorf("webview: native file drops require GTK 4.6+ GdkFileList support")
}
