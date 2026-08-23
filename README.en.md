# purego-webview

No-CGO webview framework for Go, powered by [purego](https://github.com/ebitengine/purego).

[中文](./README.md)

## Features

- **Zero CGO** -- cross-compiles like any pure Go project, no C toolchain required
- **Three platforms** -- macOS (WKWebView) / Windows (WebView2) / Linux (WebKitGTK)
- **Full JS bridge** -- call Go functions from JavaScript, return results via Promises
- **Custom URL Schemes** -- `app://`-style origins with secure context, no port needed
- **JS injection** -- `Init(js)` runs JavaScript before every page load
- **Pre-Run buffering** -- `SetTitle`, `SetHTML`, `Navigate` can be called before `Run()`
- **Embedded WebView2Loader.dll** -- per-architecture (amd64/arm64/x86), auto-extracted to temp
- **Native file picker** -- `<input type=file>` maps to NSOpenPanel on macOS, with `accept` attribute filtering (MIME types, extensions, wildcards)
- **Incognito mode** -- in-memory data store, no cookies/cache persisted to disk

## Platform Status

| Platform | Backend | Status |
|----------|---------|--------|
| macOS | WKWebView + AppKit (purego) | Working |
| Windows | WebView2 (COM interop) | Working |
| Linux | WebKitGTK (purego) | Working |

## Requirements

- Go 1.24+
- macOS 10.13+
- Windows 10+ (with [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) installed)
- Linux: WebKitGTK required (see below)

### Linux WebKitGTK Installation

```bash
# Debian / Ubuntu
apt install libwebkit2gtk-4.1-0

# Fedora
dnf install webkit2gtk4.1

# Arch
pacman -S webkit2gtk-4.1
```

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
| `Init` | `func (w *W) Init(js string) error` | Inject JS that runs before every page load |
| `Bind` | `func (w *W) Bind(name string, fn any) error` | Expose a Go function to JS |
| `Unbind` | `func (w *W) Unbind(name string)` | Remove a bound JS function |
| `InterceptResource` | `func (w *W) InterceptResource(scheme string, handler ResourceHandler)` | Register custom URL scheme resource handler |

### SizeHint

| Constant | Value | Description |
|----------|-------|-------------|
| `SizeNone` | 0 | No constraint |
| `SizeMin` | 1 | Minimum size |
| `SizeMax` | 2 | Maximum size |
| `SizeFixed` | 3 | Fixed size |

### Init (JS Injection)

```go
func (w *W) Init(js string) error
```

Registers JavaScript to run before every page load. Can be called multiple times; scripts execute in registration order. Useful for polyfills, global variable interception, etc.

```go
w.Init(`console.log('page loading...')`)
w.Init(`window.__APP_VERSION = '1.0.0'`)
```

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

```go
w.Bind("add", func(a, b int) int { return a + b })
w.Bind("readFile", func(path string) (string, error) {
    data, err := os.ReadFile(path)
    return string(data), err
})
```

JavaScript side:

```javascript
const sum = await add(1, 2);
try {
    const content = await readFile("/etc/hosts");
} catch (e) {
    console.error(e.message);
}
```

### InterceptResource (Custom URL Scheme)

```go
func (w *W) InterceptResource(scheme string, handler ResourceHandler)
```

Register a resource handler for a custom URL scheme. Must be called before `Run()`.

Custom schemes like `app://` are treated as **secure contexts** on all platforms (`localStorage`, `crypto.subtle`, `getUserMedia` all work), without opening a port.

```go
w.InterceptResource("app", func(req webview.ResourceRequest, respond func(*webview.ResourceResponse)) {
    if strings.Contains(req.URL, "index.html") {
        respond(&webview.ResourceResponse{
            StatusCode: 200,
            Headers:    map[string]string{"Content-Type": "text/html"},
            Body:       []byte(`<h1>Hello</h1>`),
        })
    } else {
        respond(nil) // 404
    }
})
w.Navigate("app://host/index.html")
```

**Type definitions:**

```go
type ResourceRequest struct {
    URL     string
    Method  string
    Headers map[string]string
}

type ResourceResponse struct {
    StatusCode int
    Headers    map[string]string
    Body       []byte
}

type ResourceHandler func(req ResourceRequest, respond func(*ResourceResponse))
```

### File Picker (macOS)

The `accept` attribute on `<input type=file>` is natively supported for file type filtering:

```html
<!-- MIME types -->
<input type="file" accept="image/png,application/pdf">

<!-- File extensions -->
<input type="file" accept=".png,.jpg,.pdf">

<!-- Wildcards -->
<input type="file" accept="image/*,video/*">

<!-- Mixed -->
<input type="file" accept="image/*,.pdf,text/plain">
```

Wildcard mapping:
| Wildcard | UTType |
|----------|--------|
| `image/*` | `UTTypeImage` |
| `video/*` | `UTTypeMovie` |
| `audio/*` | `UTTypeAudio` |
| `text/*` | `UTTypeText` |

## cmd/app -- Universal App Shell

`cmd/app` is a universal desktop app launcher that loads frontend resources + config from a zip file or directory.

**Directory structure:**
```
app.data (zip) or data/
├── config.json
└── dist/
    └── index.html
```

**config.json:**
```json
{
  "title": "My App",
  "width": 1024,
  "height": 768,
  "resizable": true,
  "debug": false,
  "incognito": true,
  "version": "1.0.0",
  "entry": "index.html",
  "dist": "dist",
  "scheme": "app"
}
```

**Run:**
```bash
# From data/ directory
go run ./cmd/app

# Build
CGO_ENABLED=0 go build -o myapp ./cmd/app
```

**JS bridge functions:**
- `app.version()` -- returns app version
- `app.close()` -- closes the window
- `app.debug(msg)` -- prints to stdout

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
- macOS / Linux: `window.webkit.messageHandlers.webviewBridge.postMessage()`
- Windows: `window.chrome.webview.postMessage()`

## Build & Run

```bash
# macOS
CGO_ENABLED=0 go run ./example

# Linux
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

If WebView2 Runtime is not installed, download it from [Microsoft](https://developer.microsoft.com/en-us/microsoft-edge/webview2/).

## License

MIT
