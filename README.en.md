# purego-webview

No-CGO webview framework for Go, powered by [purego](https://github.com/ebitengine/purego).

[中文](./README.md)

## Features

- **Zero CGO** -- cross-compiles like any pure Go project, no C toolchain required
- **macOS** -- WKWebView + AppKit via purego (ObjC runtime)
- **Windows** -- WebView2 via pure COM interop (syscall.SyscallN)
- **Full JS bridge** -- call Go functions from JavaScript, return results via Promises
- **Pre-Run buffering** -- `SetTitle`, `SetHTML`, `Navigate` can be called before `Run()`
- **Embedded WebView2Loader.dll** -- per-architecture (amd64/arm64/x86), auto-extracted to temp
- **Native file picker** -- `<input type=file>` maps to NSOpenPanel on macOS
- **Incognito mode** -- in-memory data store, no cookies/cache persisted to disk

## Platform Status

| Platform | Backend | Status |
|----------|---------|--------|
| macOS | WKWebView + AppKit (purego) | Working |
| Windows | WebView2 (COM interop) | Working |
| Linux | WebKitGTK | Planned |

## Requirements

- Go 1.24+
- macOS 10.13+ or Windows 10+ (with [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) installed)

## Install

```bash
go get github.com/opentoys/webview
```

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

```bash
CGO_ENABLED=0 go run ./example
```

## API

### Create

```go
func New(opts Options) (*W, error)
```

Create a new webview window. Returns a handle `*W`.

```go
w, err := webview.New(webview.Options{
    Debug:     true,     // enable dev tools
    Incognito: true,     // in-memory data store
    DataDir:   "./data", // custom data directory (Windows default: %AppData%\<exe>)
})
```

### Control

| Method | Signature | Description |
|--------|-----------|-------------|
| `Run` | `func (w *W) Run() error` | Start event loop, blocks until window closes |
| `Close` | `func (w *W) Close() error` | Close window |
| `SetTitle` | `func (w *W) SetTitle(title string)` | Set window title |
| `SetSize` | `func (w *W) SetSize(w, h int, hint SizeHint)` | Set window size |
| `Navigate` | `func (w *W) Navigate(url string) error` | Navigate to URL |
| `SetHTML` | `func (w *W) SetHTML(html string) error` | Load HTML string |
| `Eval` | `func (w *W) Eval(js string) error` | Execute JavaScript |

### SizeHint

| Constant | Value | Description |
|----------|-------|-------------|
| `SizeNone` | 0 | No constraint |
| `SizeMin` | 1 | Minimum size |
| `SizeMax` | 2 | Maximum size |
| `SizeFixed` | 3 | Fixed size |

### Bind (JS Bridge)

```go
func (w *W) Bind(name string, fn any) error
```

Expose a Go function to JavaScript. The function becomes a global Promise-returning function in the webview.

**Supported return signatures:**
- `func(args...)` -- no return, Promise resolves to `undefined`
- `func(args...) T` -- returns value, Promise resolves to `T`
- `func(args...) (T, error)` -- returns value or error, rejects on error
- `func(args...) error` -- returns only error

**Argument types:** Any JSON-serializable Go type. JS arguments decoded via `encoding/json`.

```go
// No return
w.Bind("log", func(msg string) {
    fmt.Println(msg)
})

// Return value
w.Bind("add", func(a, b int) int {
    return a + b
})

// Return value + error
w.Bind("readFile", func(path string) (string, error) {
    data, err := os.ReadFile(path)
    return string(data), err
})
```

JavaScript side:

```javascript
// All bound functions return Promises
await log("hello from JS");
const sum = await add(1, 2);
try {
    const content = await readFile("/etc/hosts");
} catch (e) {
    console.error(e.message);
}
```

## JS Bridge Protocol

The bridge uses a JSON message protocol between JS and Go.

**JS -> Go (request):**
```json
{"id": 1, "name": "add", "args": [1, 2]}
```

**Go -> JS (response):**
```javascript
webviewBridge.resolve(1, 3)       // success
webviewBridge.reject(1, "error")  // failure
```

Transport:
- macOS: `window.webkit.messageHandlers.webviewBridge.postMessage()`
- Windows: `window.chrome.webview.postMessage()`

## Build & Run

```bash
# macOS
CGO_ENABLED=0 go run ./example

# Windows (cross-compile from macOS/Linux)
GOOS=windows GOARCH=amd64 go build -o app.exe ./example

# Or build natively on Windows
go build -o app.exe ./example
```

## Test

```bash
CGO_ENABLED=0 go test ./...
```

## Windows WebView2 Loader

WebView2 requires `WebView2Loader.dll` to bootstrap. The library handles this automatically.

**Search order:**
1. `X_WEBVIEW2LOADER_DLL` environment variable (explicit path)
2. Embedded DLL (per-architecture, hash-based cache in temp dir)
3. System DLL (PATH + exe directory)
4. Explicit exe directory search

**Embedded architectures:** amd64, arm64, x86. Other architectures fall back to system DLL.

If WebView2 Runtime is not installed, download it from [Microsoft](https://developer.microsoft.com/en-us/microsoft-edge/webview2/).

## Examples

### Counter

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

### Navigate to URL

```go
package main

import "github.com/opentoys/webview"

func main() {
	w, _ := webview.New(webview.Options{Debug: true})
	defer w.Close()

	w.SetTitle("browser")
	w.SetSize(1024, 768, webview.SizeNone)
	w.Navigate("https://example.com")

	w.Run()
}
```

### Incognito + Custom DataDir

```go
w, _ := webview.New(webview.Options{
    Incognito: true,              // no persistent storage
    DataDir:   "./my-app-data",   // custom data directory
})
```

## License

MIT
