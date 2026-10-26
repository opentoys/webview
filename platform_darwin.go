//go:build darwin

package webview

import (
	"github.com/opentoys/webview/internal/darwin"
	"github.com/opentoys/webview/internal/debuglog"
)

// CmdOrCtrl is "Cmd" on macOS, intended for use in MenuItem.Shortcut.
// Example: Shortcut: webview.CmdOrCtrl + "+Z"
const CmdOrCtrl = "Cmd"

// DefaultMenus returns the platform's default menu bar.
// On macOS this is an Edit menu with Undo, Redo, Cut, Copy, Paste, Select All.
// Editing commands use AppKit's responder chain so WKWebView handles them as
// native actions (including paste, without the web clipboard permission flow).
func DefaultMenus(w *W) []Menu {
	return []Menu{
		{
			Items: []MenuItem{
				{Label: "Quit", Shortcut: CmdOrCtrl + "+Q", Action: func() {
					w.Close()
				}},
			},
		},
		{
			Label: "Edit",
			Items: []MenuItem{
				{Label: "Undo", Shortcut: CmdOrCtrl + "+Z", Role: MenuItemRoleUndo},
				{Label: "Redo", Shortcut: CmdOrCtrl + "+Shift+Z", Role: MenuItemRoleRedo},
				{Separator: true},
				{Label: "Cut", Shortcut: CmdOrCtrl + "+X", Role: MenuItemRoleCut},
				{Label: "Copy", Shortcut: CmdOrCtrl + "+C", Role: MenuItemRoleCopy},
				{Label: "Paste", Shortcut: CmdOrCtrl + "+V", Role: MenuItemRolePaste},
				{Separator: true},
				{Label: "Select All", Shortcut: CmdOrCtrl + "+A", Role: MenuItemRoleSelectAll},
			},
		},
	}
}

func buildPlatform(opts Options, w *W, logger *debuglog.Logger) (Platform, error) {
	p, e := darwin.New()
	if e != nil {
		return nil, e
	}
	p.Debug = opts.Debug
	p.Logger = logger
	p.Incognito = opts.Incognito
	p.DataDir = opts.DataDir
	p.Offscreen = opts.Offscreen
	p.BoundFuncs = w.bridge.funcNames
	p.MessageFunc = func(body string) {
		w.bridge.HandleMessage(body, p.EvalHost)
	}
	// Capture <input accept> on click so the native file picker can read it
	// synchronously before showing the panel.
	w.bridge.Bind("__accept__", func(v string) {
		darwin.SetAccept(v)
	})
	p.Init(`document.addEventListener('click', function(e) {
		var el = e.target;
		if (el && el.tagName === 'INPUT' && el.type === 'file') {
			__accept__(el.accept || '');
		}
	}, true);`)
	return p, nil
}
