package main

import (
	"github.com/opentoys/webview"
	"github.com/opentoys/webview/internal/types"
)

var baiduw *webview.W
var googlew *webview.W

func main() {
	tray, err := webview.NewSystray(webview.SystrayOptions{
		Icon:    []byte{},
		Title:   "WebApp",
		Tooltip: "123123123123",
		Items: []types.MenuItem{
			{
				ID:    "baidu",
				Label: "百度",
				Action: func() {
					if baiduw == nil {
						baiduw = createWindow("https://www.baidu.com")
						return
					}
					baiduw.Show()
				},
			},
			{
				ID:    "google",
				Label: "谷歌",
				Action: func() {
					if googlew == nil {
						googlew = createWindow("https://www.google.com")
						return
					}
					googlew.Show()
				},
			},
			{
				ID:    "quit",
				Label: "退出",
				Action: func() {
					webview.Quit()
				},
			},
		},
	})

	if err != nil {
		panic(err)
	}
	defer tray.Close()

	webview.Run()
}

func createWindow(url string) *webview.W {
	w, err := webview.New(webview.Options{Debug: true})
	if err != nil {
		panic(err)
	}
	// defer w.Close()/

	w.OnClose(func() {
		w.Hidden()
	})

	w.SetTitle(url)
	w.SetSize(480, 360, webview.SizeNone)
	w.Navigate(url)
	w.Show()
	return w
}
