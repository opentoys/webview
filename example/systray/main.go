// Hide on close, retain DOM/JS state, and restore from an independent tray.
package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"

	"github.com/opentoys/webview"
)

func report(err error) {
	if err != nil {
		log.Print(err)
	}
}
func main() {
	w, err := webview.New(webview.Options{})
	if err != nil {
		log.Fatal(err)
	}
	report(w.SetTitle("Hide and restore"))
	report(w.Init(`window.initializations = (window.initializations || 0) + 1;`))
	report(w.SetHTML(`<!doctype html><meta charset="utf-8"><h1>Hide and restore</h1><p>Type below, close the window, then click the tray. The input and JS state remain.</p><input value="kept across hiding"><button onclick="document.querySelector('output').textContent=++window.count">Count</button><output>0</output><script>window.count=0</script>`))
	report(w.OnClose(func() { report(w.Hidden()) }))
	icon := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			icon.Set(x, y, color.RGBA{40, 130, 230, 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, icon); err != nil {
		log.Fatal(err)
	}
	_, err = webview.NewSystray(webview.SystrayOptions{
		Title: "运行中", Icon: data.Bytes(), Tooltip: "Click to restore the window",
		OnClick: func() { report(w.Show()) },
		Items: []webview.MenuItem{
			{ID: "show", Label: "Show window", Action: func() { report(w.Show()) }},
			{ID: "quit", Label: "Quit application", Action: func() { report(webview.Quit()) }},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	report(webview.Run())
}
