package main

import (
	"fmt"

	"github.com/opentoys/webview"
)

func main() {
	w, err := webview.New(webview.Options{
		Debug: true,
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
	// w.Navigate("http://localhost:5173")
	w.Navigate("https://developer.mozilla.org/zh-CN/docs/Web/API/Permissions_API/Using_the_Permissions_API")
	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
