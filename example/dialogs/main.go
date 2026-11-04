// Select a file before Run, then use native dialogs from a tray without a WebView.
package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log"

	"github.com/opentoys/webview"
)

func report(path string, err error) {
	if errors.Is(err, webview.ErrDialogCancelled) {
		return
	}
	if err != nil {
		log.Print(err)
		return
	}
	log.Print(path)
}

func main() {
	// Keep this call on the main goroutine for macOS.
	report(webview.ChooseFile(webview.FileFilter{Name: "Text", Extensions: []string{"txt", "md"}}))
	icon := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			icon.Set(x, y, color.RGBA{R: 70, G: 150, B: 230, A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, icon); err != nil {
		log.Fatal(err)
	}
	_, err := webview.NewSystray(webview.SystrayOptions{
		Title: "Dialogs", Icon: data.Bytes(), Tooltip: "Independent native dialogs",
		Items: []webview.MenuItem{
			{ID: "directory", Label: "Choose directory…", Action: func() { report(webview.ChooseDir()) }},
			{ID: "open", Label: "Open file…", Action: func() { report(webview.ChooseFile()) }},
			{ID: "save", Label: "Save file…", Action: func() {
				report(webview.SaveFile(webview.FileDialogOptions{
					Title:    "Save report",
					Filename: "report.txt",
					Filters:  []webview.FileFilter{{Name: "Text", Extensions: []string{"txt"}}},
				}))
			}},
			{ID: "quit", Label: "Quit", Action: func() {
				if err := webview.Quit(); err != nil {
					log.Print(err)
				}
			}},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := webview.Run(); err != nil {
		log.Fatal(err)
	}
}
