# purego-webview

No-CGO webview framework for Go, powered by [purego](https://github.com/ebitengine/purego).

## Features

- macOS WKWebView backend (working)
- Full JS bridge: call Go from JavaScript, return results via promises
- JS dialog handling (alert, confirm, prompt)
- `<input type=file>` maps to a native macOS file picker (single/multiple)
- Zero cgo dependency -- cross-compiles like any pure Go project
- Windows (WebView2) and Linux (WebKitGTK) planned

## Quick Start

```go
package main

import "github.com/opentoys/webview"

func main() {
	w, _ := webview.New(webview.Options{Debug: true})
	defer w.Close()

	count := 0
	w.Bind("increment", func() int {
		count++
		return count
	})

	w.SetTitle("counter")
	w.SetSize(600, 400, webview.SizeNone)
	w.SetHTML(`<!doctype html>
<html><body style="text-align:center;padding-top:2em">
  <p id="c" style="font-size:2em">0</p>
  <button onclick="increment().then(n =>
    document.getElementById('c').textContent = n)">+1</button>
</body></html>`)

	w.Run()
}
```

## Build & Run

```bash
CGO_ENABLED=0 go get github.com/opentoys/webview
CGO_ENABLED=0 go run ./example
```

Or build a binary:

```bash
CGO_ENABLED=0 go build -o counter ./example
./counter
```

## API

| Method | Description |
|---|---|
| `New(opts Options) (*W, error)` | Create a new webview window |
| `w.Run()` | Start the event loop (blocks until window closes) |
| `w.Close()` | Close the window |
| `w.SetTitle(title)` | Set window title |
| `w.SetSize(w, h, hint)` | Set window size (SizeNone, SizeMin, SizeMax, SizeFixed) |
| `w.SetHTML(html)` | Load HTML content |
| `w.Navigate(url)` | Navigate to a URL |
| `w.Eval(js)` | Execute JavaScript |
| `w.Bind(name, fn)` | Expose a Go function to JavaScript (returns a Promise) |
| `w.SetDialogHandler(fn)` | Override the default JS dialog handler |
| `w.SetOpenPanelHandler(fn)` | Replace the native file picker for `<input type=file>` |
| `<input type=file>` | Opens a native macOS file picker (modal) |

## Status

| Platform | Backend | Status |
|---|---|---|
| macOS | WKWebView + AppKit | Working |
| Windows | WebView2 | Planned |
| Linux | WebKitGTK | Planned |

**Note:** The top-level `webview.go` directly imports the `platform/darwin` package, so cross-compilation to Windows/Linux will fail until platform-specific build tags are added. This is expected for the current macOS-only release.

## Requirements

- Go 1.24+
- macOS (for now)

## Test

```bash
CGO_ENABLED=0 go test ./...
```

## License

MIT
