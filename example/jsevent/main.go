package main

import (
	"encoding/json"
	"fmt"
	"os"

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

	if err := w.OnFilesDropped(func(files []string) {
		var paths []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"is_dir"`
		}
		for _, v := range files {
			f, e := os.Stat(v)
			if e != nil {
				continue
			}
			paths = append(paths, struct {
				Name  string "json:\"name\""
				IsDir bool   "json:\"is_dir\""
			}{
				Name:  v,
				IsDir: f.IsDir(),
			})
		}
		buf, _ := json.Marshal(paths)
		w.Eval(`document.dispatchEvent(new CustomEvent('appdrop', {detail: ` + string(buf) + `}))`)
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
  <h3>Drop demo and js event</h3>
  <p>Uses <code>DefaultMenus(w)</code> for the Edit menu and appends File + Help.
     Shortcuts use <code>webview.CmdOrCtrl</code> (Cmd on macOS, Ctrl elsewhere).</p>
  <textarea id="box">Type here to test Cut/Copy/Paste/Undo/Redo.</textarea>
</body>
<script>
document.addEventListener('appdrop',function(e){
	console.log(e)
	alert(JSON.stringify(e.detail))
})
</script>
</html>`)

	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
