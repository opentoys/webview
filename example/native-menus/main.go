package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"sync"
	"sync/atomic"

	"github.com/opentoys/webview"
)

func main() {
	w, err := webview.New(webview.Options{})
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()
	report := func(err error) {
		if err != nil {
			log.Print(err)
		}
	}
	icon := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 4; y < 28; y++ {
		for x := 4; x < 28; x++ {
			icon.Set(x, y, color.RGBA{40, 130, 230, 255})
		}
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, icon); err != nil {
		log.Fatal(err)
	}
	var count atomic.Int64
	var trayMu sync.Mutex
	var nativeTray *webview.Systray
	removeTray := func() error {
		trayMu.Lock()
		defer trayMu.Unlock()
		if nativeTray == nil {
			return nil
		}
		err := nativeTray.Close()
		nativeTray = nil
		return err
	}
	var tray func() error
	tray = func() error {
		trayMu.Lock()
		defer trayMu.Unlock()
		opts := webview.SystrayOptions{Icon: pngData.Bytes(), Title: fmt.Sprintf("WebView %d", count.Load()), Tooltip: fmt.Sprintf("WebView tray — updated %d times", count.Load()), Items: []webview.MenuItem{
			{ID: "update", Label: "Update tray", Action: func() { count.Add(1); report(tray()) }},
			{ID: "remove", Label: "Remove tray", Action: func() { report(removeTray()) }},
			{ID: "separator", Separator: true},
			{ID: "quit", Label: "Quit", Action: func() { report(webview.Quit()) }},
		}}
		if nativeTray == nil {
			var err error
			nativeTray, err = webview.NewSystray(opts)
			return err
		}
		return nativeTray.Update(opts)
	}
	report(tray())
	for _, item := range []webview.MenuItem{
		{ID: "hello", Label: "Go callback", Action: func() { report(w.Eval(`alert('Hello from Go')`)) }},
		{ID: "remove-self", Label: "Remove this item", Action: func() { report(w.RemoveContextMenuItem("remove-self")) }},
	} {
		report(w.AddContextMenuItem(item))
	}
	report(w.RemoveContextMenuItemRole(webview.MenuItemRoleCopy))
	report(w.Bind("restoreTray", tray))
	report(w.Bind("removeTray", removeTray))
	report(w.Bind("addItem", func() error {
		return w.AddContextMenuItem(webview.MenuItem{ID: "added", Label: "Added at runtime", Action: func() { report(w.Eval(`alert('Added item clicked')`)) }})
	}))
	report(w.Bind("removeItem", func() error { return w.RemoveContextMenuItem("added") }))
	report(w.Bind("resetContext", func() error { return w.SetContextMenu(nil) }))
	w.SetTitle("Native tray and context menus")
	report(w.SetHTML(`<!doctype html><meta charset="utf-8"><h1>Native menus</h1>
 <p>Select this text and right-click. Copy is hidden; other browser actions remain.</p>
 <input value="Try editing and right-clicking here"><p>
 <button onclick="restoreTray().catch(alert)">Create/update tray</button>
 <button onclick="removeTray().catch(alert)">Remove tray</button>
 <button onclick="addItem().catch(alert)">Add context item</button>
 <button onclick="removeItem().catch(alert)">Remove context item</button>
 <button onclick="resetContext().catch(alert)">Disable context menu</button>`))
	if err := w.Run(); err != nil {
		log.Print(err)
	}
}
