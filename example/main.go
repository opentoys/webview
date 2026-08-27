package main

import (
	"fmt"
	"net/http"

	"github.com/opentoys/webview"
)

const pageHTML = `<!doctype html>
<html>
<body style="font-family:system-ui;text-align:center;padding-top:2em">
	<h1>PureGo WebView</h1>
	<p id="c" style="font-size:2em">0</p>
	<button onclick="increment().then(n => document.getElementById('c').textContent = n)"
		style="font-size:1.2em;padding:0.5em 1em">+1</button>
	<hr>
	<h2>File Input Test</h2>
	<input type="file" id="fileInput" accept="image/*" multiple>
	<p id="fileInfo"></p>
	<div>
		<!-- Same-origin attachment link: WebKitGTK routes it through its
		     download machinery (download-started -> decide-destination ->
		     our native save dialog). -->
		<a href="/dl-text?name=hello.txt&text=Hello%2C%20World%21" download>Download Text</a>
	</div>
	<div>
		<a href="/dl" download>Download file (intercepts native dialog)</a>
	</div>
	<div>
		<a href="http://www.baidu.com">百度 (cross-origin nav)</a>
	</div>
	<hr>
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
</html>`

func main() {
	w, err := webview.New(webview.Options{
		Debug: true,
	})
	if err != nil {
		panic(err)
	}
	defer w.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.Write([]byte(pageHTML))
	})
	mux.HandleFunc("/dl", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Disposition", `attachment; filename="hello.txt"`)
		rw.Write([]byte("hello from download"))
	})
	// Client-generated text download, echoed back as an HTTP attachment so
	// WebKit2GTK routes it through the native save dialog (data:/blob: URLs
	// are NOT routed through the download machinery).
	mux.HandleFunc("/dl-text", func(rw http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			name = "download.txt"
		}
		text := r.URL.Query().Get("text")
		rw.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		rw.Write([]byte(text))
	})
	go func() {
		if err := http.ListenAndServe("127.0.0.1:8080", mux); err != nil {
			fmt.Println("server:", err)
		}
	}()

	count := 0
	if err := w.Bind("increment", func() int {
		count++
		return count
	}); err != nil {
		panic(err)
	}

	w.SetTitle("purego webview counter")
	w.SetSize(600, 400, webview.SizeNone)

	// Navigate to the same-origin server so download links are same-origin and
	// WebKit2GTK reliably fires download-started for attachment responses.
	if err := w.Navigate("http://127.0.0.1:8080/"); err != nil {
		panic(err)
	}
	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
