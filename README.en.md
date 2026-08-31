# purego-webview

A no-CGO desktop WebView framework for Go, powered by [purego](https://github.com/ebitengine/purego).

[中文](./README.md)

## Features

- No CGO or C toolchain; cross-compile like a regular Go project
- Native backends: WKWebView on macOS, WebView2 on Windows, and WebKitGTK on Linux
- Optional Chrome/Chromium backend driven through the Chrome DevTools Protocol
- Promise-based JavaScript-to-Go bridge
- Native menus, shortcuts, and callbacks
- Custom URL schemes for embedded frontend assets without a local HTTP server
- JavaScript injection before each page load
- Incognito mode and custom browser data directories
- System file picker through `<input type="file">`
- Embedded WebView2Loader.dll for Windows (amd64, arm64, and x86)

## Platforms and requirements

| System | Native backend | Runtime dependency |
| --- | --- | --- |
| macOS 10.13+ | WKWebView + AppKit | System frameworks |
| Windows 10+ | WebView2 | [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) |
| Linux | WebKitGTK 4.1 | WebKitGTK shared libraries |

Go 1.25+ is required. On Linux, install the runtime library:

```bash
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-0

# Fedora
sudo dnf install webkit2gtk4.1

# Arch Linux
sudo pacman -S webkit2gtk-4.1
```

The Chrome backend requires Chrome or Chromium. The executable is discovered automatically, or it can be specified with `WEBVIEW_CHROME`.

## Installation

```bash
go get github.com/opentoys/webview
```

## Quick start

```go
package main

import (
	"log"

	"github.com/opentoys/webview"
)

func main() {
	w, err := webview.New(webview.Options{Debug: true})
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	count := 0
	if err := w.Bind("increment", func() int {
		count++
		return count
	}); err != nil {
		log.Fatal(err)
	}

	w.SetTitle("Counter")
	w.SetSize(600, 400, webview.SizeNone)
	if err := w.SetHTML(`<!doctype html>
<html>
<body style="font-family:system-ui;text-align:center;padding-top:2em">
  <p id="count" style="font-size:2em">0</p>
  <button onclick="increment().then(n => count.textContent = n)">+1</button>
</body>
</html>`); err != nil {
		log.Fatal(err)
	}

	if err := w.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Run the repository example with:

```bash
CGO_ENABLED=0 go run ./example
```

## Options and backend selection

```go
w, err := webview.New(webview.Options{
	Debug:     true,                     // developer tools or debug output
	Incognito: true,                     // temporary/incognito browser data
	DataDir:   "./browser-data",         // custom browser data directory
	Backend:   webview.BackendWebview,   // rendering backend
})
```

| Constant | Value | Selection order |
| --- | --- | --- |
| `BackendWebview` | `""` | Platform-native backend (default) |
| `BackendChrome` | `"chrome"` | Chrome/Chromium only |
| `BackendFallbackWebview` | `"fallback-webview"` | Chrome first, then native if creation fails |
| `BackendFallbackChrome` | `"fallback-chrome"` | Native first, then Chrome if creation fails |

Backend probing happens during `New`. Some native shared libraries are not loaded until `Run`, so failures at that stage cannot trigger an automatic backend switch.

## API overview

| Method | Description |
| --- | --- |
| `Run() error` | Start the event loop and block until the window closes |
| `Close() error` | Close the window |
| `SetTitle(title)` | Set the window title |
| `SetSize(width, height, hint)` | Set window size constraints |
| `Navigate(url) error` | Navigate to a URL |
| `SetHTML(html) error` | Load an HTML string |
| `Eval(js) error` | Execute JavaScript in the current page |
| `Init(js) error` | Register JavaScript to run before every page load |
| `Bind(name, fn) error` | Expose a Go function to JavaScript |
| `Unbind(name)` | Remove a binding |
| `SetMenu(menus...)` | Replace the native menu bar |
| `MainThread(fn)` | Run a function on the UI thread and wait for it |
| `InterceptResource(scheme, handler)` | Register a custom URL scheme handler; call before `Run` |

`SetTitle`, `SetSize`, `Navigate`, `SetHTML`, and `Init` may be called before `Run`.

### Window size

| Constant | Meaning |
| --- | --- |
| `SizeNone` | Normal window size |
| `SizeMin` | Set the minimum size |
| `SizeMax` | Set the maximum size |
| `SizeFixed` | Fixed size |

### JavaScript bridge

`Bind` accepts these return forms:

- `func(args...)`
- `func(args...) T`
- `func(args...) error`
- `func(args...) (T, error)`

Arguments and results are converted through JSON. A bound function becomes a global JavaScript function that always returns a Promise:

```go
if err := w.Bind("add", func(a, b int) int {
	return a + b
}); err != nil {
	log.Fatal(err)
}

if err := w.Bind("readFile", func(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}); err != nil {
	log.Fatal(err)
}
```

```javascript
const sum = await add(1, 2);

try {
  const content = await readFile("notes.txt");
} catch (error) {
  console.error(error.message);
}
```

### Custom URL schemes

Resource interception is useful for serving files from `embed.FS`, zip archives, or other data sources. The scheme excludes `://`. The handler must call `respond` once; pass `nil` when the resource is not found.

```go
w.InterceptResource("app", func(
	req webview.ResourceRequest,
	respond func(*webview.ResourceResponse),
) {
	if req.URL != "app://host/index.html" {
		respond(nil)
		return
	}

	respond(&webview.ResourceResponse{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type": {"text/html; charset=utf-8"},
		},
		Body: []byte("<h1>Hello</h1>"),
	})
})

if err := w.Navigate("app://host/index.html"); err != nil {
	log.Fatal(err)
}
```

Related types:

```go
type ResourceRequest struct {
	URL     string
	Method  string
	Headers http.Header
	Body    []byte
}

type ResourceResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}
```

Custom schemes are registered as secure contexts by each backend, enabling APIs such as `localStorage` and `crypto.subtle` without listening on a local port.

### Native menus

```go
menus := webview.DefaultMenus(w)
menus = append(menus, webview.Menu{
	Label: "File",
	Items: []webview.MenuItem{
		{
			Label:    "Open",
			Shortcut: webview.CmdOrCtrl + "+O",
			Action:   func() { /* ... */ },
		},
		{Separator: true},
		{
			Label:    "Quit",
			Shortcut: webview.CmdOrCtrl + "+Q",
			Action:   func() { _ = w.Close() },
		},
	},
})
w.SetMenu(menus...)
```

`CmdOrCtrl` is `Cmd` on macOS and `Ctrl` elsewhere. `DefaultMenus` returns the application and Edit menus on macOS, and an empty slice on Linux and Windows. The Chrome backend currently ignores `SetMenu`.

### UI thread

Use `MainThread` for window or system APIs that must execute on the platform UI thread:

```go
w.MainThread(func() {
	// macOS: AppKit thread
	// Windows: Win32 message-loop thread
	// Linux: GTK main thread
})
```

## Universal app shell: `cmd/app`

`cmd/app` loads an application from `app.data`, a zip archive in the current directory. If the archive is absent, it falls back to the `data/` directory. Both sources use the same layout:

```text
app.data (zip) or data/
├── config.json
└── dist/
    ├── index.html
    └── ...
```

Example `config.json`:

```json
{
  "title": "My App",
  "width": 1024,
  "height": 720,
  "debug": false,
  "incognito": false,
  "dir": "./browser-data",
  "version": "1.0.0",
  "entry": "index.html",
  "dist": "dist",
  "scheme": "app"
}
```

```bash
CGO_ENABLED=0 go run ./cmd/app
```

The shell binds `app.version()`, `app.close()`, and `app.debug(message)` into the page. See [`cmd/app`](./cmd/app) for the complete implementation.
