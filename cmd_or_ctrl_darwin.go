//go:build darwin

package webview

// CmdOrCtrl is "Cmd" on macOS, intended for use in MenuItem.Shortcut.
// Example: Shortcut: webview.CmdOrCtrl + "+Z"
const CmdOrCtrl = "Cmd"
