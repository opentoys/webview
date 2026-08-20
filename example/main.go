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
		fmt.Println(count)
		return count
	}); err != nil {
		panic(err)
	}

	w.SetTitle("purego webview counter")
	w.SetSize(600, 400, webview.SizeNone)
	w.SetHTML(`<!doctype html>
<html>
<body style="font-family:system-ui;text-align:center;padding-top:2em">
	<h1>PureGo WebView</h1>
	<p id="c" style="font-size:2em">0</p>
	<button onclick="increment().then(n => document.getElementById('c').textContent = n)"
		style="font-size:1.2em;padding:0.5em 1em">+1</button>
	<hr>
	<h2>File Input Test</h2>
	<input type="file" id="fileInput" multiple>
	<p id="fileInfo"></p>
	<script>
	document.getElementById('fileInput').addEventListener('change', function(e) {
		var files = e.target.files;
		var info = files.length + ' file(s) selected:';
		for (var i = 0; i < files.length; i++) {
			info += '\n  ' + files[i].name + ' (' + files[i].size + ' bytes)';
		}
		document.getElementById('fileInfo').textContent = info;
	});
	</script>
</body>
</html>`)
	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
