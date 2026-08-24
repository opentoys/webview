# purego-webview

No-CGO webview framework for Go, powered by [purego](https://github.com/ebitengine/purego).

[中文](./README.md)

## Features

- **Zero CGO** -- cross-compiles like any pure Go project, no C toolchain required
- **Three platforms** -- macOS (WKWebView) / Windows (WebView2) / Linux (WebKitGTK)
- **Full JS bridge** -- call Go functions from JavaScript, return results via Promises
- **Native menus** -- platform-native menu bar with keyboard shortcuts and callbacks
- **Custom URL Schemes** -- `app://`-style origins with secure context, no port needed
- **JS injection** -- `Init(js)` runs JavaScript before every page load
- **Pre-Run buffering** -- `SetTitle`, `SetHTML`, `Navigate` can be called before `Run()`
- **Native file picker** -- `<input type=file>` maps to system file picker with `accept` filtering
- **Incognito mode** -- in-memory data store, no cookies/cache persisted to disk
- **Embedded WebView2Loader.dll** -- per-architecture (amd64/arm64/x86), auto-extracted to temp

## Platform Status

| Platform | Backend | Status |
|----------|---------|--------|
| macOS | WKWebView + AppKit (purego) | Working |
| Windows | WebView2 (COM interop) | Working |
| Linux | WebKitGTK (purego) | Working |

## Requirements

- Go 1.25+
- macOS 10.13+
- Windows 10+ (with [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) installed)
- Linux: WebKitGTK required:

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
w, err := webview.New(webview.Options{
    Debug:     true,     // enable dev tools
    Incognito: true,     // in-memory data store
    DataDir:   "./data", // custom data directory (Windows default: %AppData%\<exe>)
})
```

### Control

| Method | Description |
|--------|-------------|
| `Run()` | Start event loop, blocks until window closes |
| `Close()` | Close window |
| `SetTitle(title)` | Set window title |
| `SetSize(w, h, hint)` | Set window size |
| `Navigate(url)` | Navigate to URL |
| `SetHTML(html)` | Load HTML string |
| `Eval(js)` | Execute JavaScript |
| `Init(js)` | Inject JS that runs before every page load |
| `Bind(name, fn)` | Expose a Go function to JS |
| `Unbind(name)` | Remove a bound JS function |
| `SetMenu(menus...)` | Set native menu bar |
| `MainThread(f)` | Run f on the platform UI thread, blocking until complete |
| `InterceptResource(scheme, handler)` | Register custom URL scheme resource handler |

### SizeHint

| Constant | Value | Description |
|----------|-------|-------------|
| `SizeNone` | 0 | No constraint |
| `SizeMin` | 1 | Minimum size |
| `SizeMax` | 2 | Maximum size |
| `SizeFixed` | 3 | Fixed size |

### Native Menus

```go
// Platform modifier key: "Cmd" on macOS, "Ctrl" elsewhere
webview.CmdOrCtrl

// Get platform default menus (Edit menu on macOS, empty on others)
menus := webview.DefaultMenus(w)

// Add custom menus
menus = append(menus, webview.Menu{
    Label: "File",
    Items: []webview.MenuItem{
        {Label: "Open", Shortcut: webview.CmdOrCtrl + "+O", Action: func() { ... }},
        {Separator: true},
        {Label: "Quit", Shortcut: webview.CmdOrCtrl + "+Q", Action: func() { w.Close() }},
    },
})

w.SetMenu(menus...)
```

**Types:**

```go
type Menu struct {
    Label string
    Items []MenuItem
}

type MenuItem struct {
    Label     string
    Shortcut  string // "Ctrl+Z", "Cmd+Shift+Z", etc.
    Action    func()
    Separator bool   // when true, other fields are ignored
}
```

### Bind (JS Bridge)

Expose a Go function to JavaScript. The function becomes a global Promise-returning function.

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

```javascript
const sum = await add(1, 2);
try {
    const content = await readFile("/etc/hosts");
} catch (e) {
    console.error(e.message);
}
```

### InterceptResource (Custom URL Scheme)

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

**Types:**

```go
type ResourceRequest struct {
    URL     string
    Method  string
    Headers map[string]string
    Body    []byte
}

type ResourceResponse struct {
    StatusCode int
    Headers    map[string]string
    Body       []byte
}
```

### MainThread (UI Thread Dispatch)

Run a function on the platform's UI thread, blocking until it completes. Use when calling platform APIs that must run on the main thread (e.g., native dialogs, window manipulation).

```go
w.MainThread(func() {
    // macOS: runs on AppKit thread
    // Windows: runs on Win32 message loop thread
    // Linux: runs on GTK main thread
})
```

### File Picker (macOS)

The `accept` attribute on `<input type=file>` is natively supported for file type filtering:

```html
<input type="file" accept="image/png,application/pdf">  <!-- MIME types -->
<input type="file" accept=".png,.jpg,.pdf">              <!-- extensions -->
<input type="file" accept="image/*,video/*">             <!-- wildcards -->
```

## cmd/app -- Universal App Shell

`cmd/app` is a desktop app launcher that loads frontend resources + config from a zip file or directory.

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
go run ./cmd/app                             # from data/ directory
CGO_ENABLED=0 go build -o myapp ./cmd/app   # build
```

**JS bridge functions:** `app.version()` / `app.close()` / `app.debug(msg)`

## Build & Run

```bash
CGO_ENABLED=0 go run ./example                           # macOS / Linux
GOOS=windows GOARCH=amd64 go build -o app.exe ./example  # cross-compile for Windows
```

## Test

```bash
CGO_ENABLED=0 go test ./...
```

## License

MIT
