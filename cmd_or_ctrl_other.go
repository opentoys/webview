//go:build !darwin

package webview

// CmdOrCtrl is "Ctrl" on Linux and Windows, intended for use in MenuItem.Shortcut.
// Example: Shortcut: webview.CmdOrCtrl + "+Z"
const CmdOrCtrl = "Ctrl"
