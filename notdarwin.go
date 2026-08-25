//go:build !darwin

package webview

// CmdOrCtrl is "Ctrl" on Linux and Windows, intended for use in MenuItem.Shortcut.
// Example: Shortcut: webview.CmdOrCtrl + "+Z"
const CmdOrCtrl = "Ctrl"

// DefaultMenus returns the platform's default menu bar.
// On Linux and Windows there is no default menu (the built-in Edit menu is
// provided automatically); this returns an empty slice.
func DefaultMenus(w *W) []Menu {
	return nil
}
