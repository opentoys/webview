//go:build !darwin

package webview

// DefaultMenus returns the platform's default menu bar.
// On Linux and Windows there is no default menu (the built-in Edit menu is
// provided automatically); this returns an empty slice.
func DefaultMenus(w *W) []Menu {
	return nil
}
