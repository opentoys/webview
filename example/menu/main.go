package main

import (
	"fmt"
	"runtime"
	"strings"

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

	// Start with platform defaults (Edit menu on macOS; empty on Linux/Windows).
	var menus = []webview.Menu{}
	if runtime.GOOS == "darwin" {
		menus = append(menus, webview.Menu{})
	}
	// Append custom menus.
	menus = append(menus,
		webview.Menu{
			Label: "File",
			Items: []webview.MenuItem{
				{Label: "New Window", Shortcut: webview.CmdOrCtrl + "+N", Action: func() {
					fmt.Println("File → New Window")
				}},
				{Separator: true},
				{Label: "Quit", Shortcut: webview.CmdOrCtrl + "+Q", Action: func() {
					w.Close()
				}},
			},
		},
	)

	menus = append(menus, webview.DefaultMenus(w)...)
	menus = append(menus, webview.Menu{
		Label: "Help",
		Items: []webview.MenuItem{
			{Label: "About", Action: func() {
				fmt.Println("Help → About")
			}},
		},
	})

	w.SetMenu(menus...)

	w.Bind("opendir", w.ChooseDir)

	if err := w.OnFilesDropped(func(paths []string) {
		w.Eval(`alert("` + strings.Join(paths, ",") + `")`)
		fmt.Println(paths)
	}); err != nil {
		panic(err)
	}

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
  <button onclick="opendir().then(v => alert(v))"
		style="font-size:1.2em;padding:0.5em 1em">Choose Dir</button>
  <p>Uses <code>DefaultMenus(w)</code> for the Edit menu and appends File + Help.
     Shortcuts use <code>webview.CmdOrCtrl</code> (Cmd on macOS, Ctrl elsewhere).</p>
  <textarea id="box">Type here to test Cut/Copy/Paste/Undo/Redo.</textarea>
</body>
</html>`)

	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
