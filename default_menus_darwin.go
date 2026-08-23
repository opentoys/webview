//go:build darwin

package webview

// DefaultMenus returns the platform's default menu bar.
// On macOS this is an Edit menu with Undo, Redo, Cut, Copy, Paste, Select All
// (all wired to document.execCommand via the given webview).
func DefaultMenus(w *W) []Menu {
	edit := func(cmd string) func() {
		return func() { w.Eval("document.execCommand('" + cmd + "')") }
	}
	return []Menu{
		{
			Label: "Edit",
			Items: []MenuItem{
				{Label: "Undo", Shortcut: CmdOrCtrl + "+Z", Action: edit("undo")},
				{Label: "Redo", Shortcut: CmdOrCtrl + "+Shift+Z", Action: edit("redo")},
				{Separator: true},
				{Label: "Cut", Shortcut: CmdOrCtrl + "+X", Action: edit("cut")},
				{Label: "Copy", Shortcut: CmdOrCtrl + "+C", Action: edit("copy")},
				{Label: "Paste", Shortcut: CmdOrCtrl + "+V", Action: edit("paste")},
				{Separator: true},
				{Label: "Select All", Shortcut: CmdOrCtrl + "+A", Action: edit("selectAll")},
			},
		},
	}
}
