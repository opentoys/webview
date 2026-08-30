package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/opentoys/webview"
)

func main() {
	w, err := webview.New(webview.Options{
		// Debug: true,
		Backend: webview.BackendFallbackWebview,
	})
	if err != nil {
		panic(err)
	}
	defer w.Close()

	w.SetTitle("purego webview counter")
	w.SetSize(600, 400, webview.SizeNone)

	logFile, _ := os.Create("app_debug.log")
	defer logFile.Close()

	w.InterceptResource("app", func(req webview.ResourceRequest, respond func(*webview.ResourceResponse)) {
		fmt.Fprintf(logFile, "resource handler called: %s %s\n", req.Method, req.URL)
		logFile.Sync()
		fmt.Println(req)
		// app://host/index.html
		// app://host/static/js/main.js
		if strings.Contains(req.URL, "index.html") {
			respond(&webview.ResourceResponse{
				StatusCode: 200,
				Headers:    http.Header{"Content-Type": []string{"text/html"}},
				Body: []byte(`<!doctype html>
<html>
<meta charset="UTF-8">
<title>purego webview counter</title>
<body style="font-family:system-ui;text-align:center;padding-top:2em">
			<script src="a.js"></script>
	<h1>PureGo WebView</h1>
	<p id="c" style="font-size:2em">0</p>
	<button onclick="increment().then(n => document.getElementById('c').textContent = n)"
		style="font-size:1.2em;padding:0.5em 1em">+1</button>
	<button onclick="testClipboard()">测试剪贴板</button>
<script>
async function testClipboard() {
    try {
        await navigator.clipboard.writeText('Hello from button click! ' + new Date().toLocaleTimeString());
        alert('✅ 写入成功！去记事本粘贴试试');
		confirm('是否读取剪贴板内容？') 
		alert(prompt('请输入内容', '默认值')+"prompt输入的内容")
    } catch (e) {
        alert('❌ 失败：' + e.message);
    }
}
</script>
</body>
</html>`),
			})
		} else {
			respond(nil) // not found, let browser handle
		}
	})

	count := 0
	if err := w.Bind("increment", func() int {
		count++
		return count
	}); err != nil {
		panic(err)
	}

	// w.Init(`document.documentElement.style.backgroundColor = '#0a0a0a';
	// document.body && (document.body.style.backgroundColor = '#0a0a0a');`)

	// w.Navigate("https://baidu.com")
	w.Navigate("app://host/index.html")
	if err := w.Run(); err != nil {
		fmt.Println("run:", err)
	}
}
