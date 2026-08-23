package main

import (
	"fmt"

	"github.com/opentoys/webview"
)

func main() {
	w, err := webview.New(webview.Options{Debug: true})
	if err != nil {
		panic(err)
	}
	defer w.Close()

	w.SetTitle("Menu Demo")
	w.SetSize(480, 360, webview.SizeNone)

	// Platform-aware shortcut prefix.
	cmdOrCtrl := "Ctrl"
	// (In a real app you'd detect macOS at runtime; here we just use Ctrl.)
	_ = cmdOrCtrl

	w.SetMenu(
		webview.Menu{
			Label: "File",
			Items: []webview.MenuItem{
				{Label: "New Window", Shortcut: "Ctrl+N", Action: func() {
					fmt.Println("File → New Window")
				}},
				{Separator: true},
				{Label: "Quit", Shortcut: "Ctrl+Q", Action: func() {
					w.Close()
				}},
			},
		},
		webview.Menu{
			Label: "Edit",
			Items: []webview.MenuItem{
				{Label: "Undo", Shortcut: "Ctrl+Z", Action: func() {
					w.Eval("document.execCommand('undo')")
				}},
				{Label: "Redo", Shortcut: "Ctrl+Y", Action: func() {
					w.Eval("document.execCommand('redo')")
				}},
				{Separator: true},
				{Label: "Cut", Shortcut: "Ctrl+X", Action: func() {
					w.Eval("document.execCommand('cut')")
				}},
				{Label: "Copy", Shortcut: "Ctrl+C", Action: func() {
					w.Eval("document.execCommand('copy')")
				}},
				{Label: "Paste", Shortcut: "Ctrl+V", Action: func() {
					w.Eval("document.execCommand('paste')")
				}},
				{Separator: true},
				{Label: "Select All", Shortcut: "Ctrl+A", Action: func() {
					w.Eval("document.execCommand('selectAll')")
				}},
			},
		},
		webview.Menu{
			Label: "Help",
			Items: []webview.MenuItem{
				{Label: "About", Action: func() {
					fmt.Println("Help → About")
				}},
			},
		},
	)

	w.SetHTML(`<!DOCTYPE html>
<html>
<head>
<style>
  body { font-family: system-ui; margin: 2em; }
  textarea { width: 100%; height: 140px; font-size: 15px; padding: 8px; }
  p { color: #555; font-size: 13px; }
</style>
</head>
<body>
  <h3>Native Menu API Demo</h3>
  <p>This example uses <code>w.SetMenu(...)</code> to build a custom menu bar
     with File, Edit, and Help menus. Try the shortcuts or click the menu items.</p>
  <textarea id="box">Type here to test Cut/Copy/Paste/Undo/Redo from the Edit menu.</textarea>
</body>
</html>`)

	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
