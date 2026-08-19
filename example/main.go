package main

import (
	"fmt"

	"github.com/opentoys/webview"
)

func main() {
	w, err := webview.New(webview.Options{
		Debug:     true,
		Incognito: false,
		DataDir:   "./userdata",
	})
	if err != nil {
		panic(err)
	}
	defer w.Close()

	count := 0
	if err := w.Bind("increment", func() int {
		count++
		return count
	}); err != nil {
		panic(err)
	}

	w.SetTitle("purego webview counter")
	w.SetSize(600, 400, webview.SizeNone)
	w.Navigate("http://localhost:5173")
	// 	w.SetHTML(`<!doctype html>
	// <html>
	// <body style="font-family:system-ui;text-align:center;padding-top:2em">
	// 	<h1>PureGo WebView</h1>
	// 	<p id="c" style="font-size:2em">0</p>
	// 	<button onclick="increment().then(n => document.getElementById('c').textContent = n)"
	// 		style="font-size:1.2em;padding:0.5em 1em">+1</button>
	// </body>
	// </html>`)
	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
