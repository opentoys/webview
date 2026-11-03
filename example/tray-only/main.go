// Start without a WebView. Each click creates a new independent window; closing
// every window leaves the tray alive, able to create more windows.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"sync/atomic"

	"github.com/opentoys/webview"
)

func report(err error) {
	if err != nil {
		log.Print(err)
	}
}
func main() {
	var count atomic.Int64
	createWindow := func() {
		w, err := webview.New(webview.Options{})
		if err != nil {
			report(err)
			return
		}
		number := count.Add(1)
		report(w.SetTitle(fmt.Sprintf("Independent window %d", number)))
		report(w.SetHTML(fmt.Sprintf(`<!doctype html><meta charset="utf-8"><h1>Window %d</h1><input value="Independent state"><p>Close this window, then create another using the tray.</p>`, number)))
	}
	icon := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			icon.Set(x, y, color.RGBA{230, 130, 40, 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, icon); err != nil {
		log.Fatal(err)
	}
	_, err := webview.NewSystray(webview.SystrayOptions{
		Title: "New window", Icon: data.Bytes(), Tooltip: "Click to create a new window", OnClick: createWindow,
		Items: []webview.MenuItem{
			{ID: "new", Label: "Create window", Action: createWindow},
			{ID: "quit", Label: "Quit application", Action: func() { report(webview.Quit()) }},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	report(webview.Run())
}
