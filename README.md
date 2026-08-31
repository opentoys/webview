# purego-webview

基于 [purego](https://github.com/ebitengine/purego) 的无 CGO Go 桌面 WebView 框架。

[English](./README.en.md)

## 特性

- 无需 CGO 或 C 工具链，支持普通 Go 交叉编译
- 原生后端：macOS WKWebView、Windows WebView2、Linux WebKitGTK
- 可选 Chrome/Chromium 后端，通过 Chrome DevTools Protocol 驱动
- JavaScript 与 Go 双向桥接，Go 函数在 JavaScript 中以 Promise 形式调用
- 原生菜单、快捷键和回调
- 自定义 URL Scheme，可直接加载内嵌前端资源，无需启动本地 HTTP 服务
- 页面加载前注入 JavaScript
- 隐身模式和自定义浏览器数据目录
- `<input type="file">` 系统文件选择器
- Windows 按架构内嵌 WebView2Loader.dll（amd64、arm64、x86）

## 平台与依赖

| 系统 | 原生后端 | 运行依赖 |
| --- | --- | --- |
| macOS 10.13+ | WKWebView + AppKit | 系统框架 |
| Windows 10+ | WebView2 | [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) |
| Linux | WebKitGTK 4.1 | WebKitGTK 动态库 |

项目要求 Go 1.25+。Linux 需安装运行时库：

```bash
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-0

# Fedora
sudo dnf install webkit2gtk4.1

# Arch Linux
sudo pacman -S webkit2gtk-4.1
```

Chrome 后端需要本机安装 Chrome 或 Chromium。程序会自动查找，也可通过 `WEBVIEW_CHROME` 指定可执行文件。

## 安装

```bash
go get github.com/opentoys/webview
```

## 快速开始

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

运行仓库中的示例：

```bash
CGO_ENABLED=0 go run ./example
```

## 配置与后端选择

```go
w, err := webview.New(webview.Options{
	Debug:     true,                     // 开启开发者工具或调试输出
	Incognito: true,                     // 使用临时/隐身浏览数据
	DataDir:   "./browser-data",         // 自定义浏览器数据目录
	Backend:   webview.BackendWebview,   // 选择渲染后端
})
```

| 常量 | 值 | 选择顺序 |
| --- | --- | --- |
| `BackendWebview` | `""` | 使用平台原生后端（默认） |
| `BackendChrome` | `"chrome"` | 仅使用 Chrome/Chromium |
| `BackendFallbackWebview` | `"fallback-webview"` | 优先 Chrome，创建失败时改用原生后端 |
| `BackendFallbackChrome` | `"fallback-chrome"` | 优先原生后端，创建失败时改用 Chrome |

后端探测发生在 `New` 阶段。部分原生动态库到 `Run` 阶段才加载，因此这类运行时初始化错误无法再自动切换后端。

## API 概览

| 方法 | 说明 |
| --- | --- |
| `Run() error` | 启动事件循环并阻塞，直到窗口关闭 |
| `Close() error` | 关闭窗口 |
| `SetTitle(title)` | 设置标题 |
| `SetSize(width, height, hint)` | 设置窗口尺寸约束 |
| `Navigate(url) error` | 导航到 URL |
| `SetHTML(html) error` | 加载 HTML 字符串 |
| `Eval(js) error` | 在当前页面执行 JavaScript |
| `Init(js) error` | 注册每次页面加载前执行的 JavaScript |
| `Bind(name, fn) error` | 将 Go 函数暴露给 JavaScript |
| `Unbind(name)` | 删除已绑定的函数 |
| `SetMenu(menus...)` | 替换原生菜单栏 |
| `MainThread(fn)` | 在 UI 线程执行函数并等待完成 |
| `InterceptResource(scheme, handler)` | 注册自定义 URL Scheme 处理器；应在 `Run` 前调用 |

`SetTitle`、`SetSize`、`Navigate`、`SetHTML` 和 `Init` 均可在 `Run` 前调用。

### 窗口尺寸

| 常量 | 含义 |
| --- | --- |
| `SizeNone` | 普通窗口尺寸 |
| `SizeMin` | 设置最小尺寸 |
| `SizeMax` | 设置最大尺寸 |
| `SizeFixed` | 固定尺寸 |

### JavaScript 桥接

`Bind` 接受以下返回形式：

- `func(args...)`
- `func(args...) T`
- `func(args...) error`
- `func(args...) (T, error)`

参数和返回值通过 JSON 转换。绑定后的函数是全局 JavaScript 函数，并始终返回 Promise：

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

### 自定义 URL Scheme

资源拦截适合从 `embed.FS`、zip 或其他数据源加载前端文件。协议名不包含 `://`，处理器必须调用一次 `respond`；传入 `nil` 表示未找到资源。

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

相关类型：

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

自定义协议在各后端中会注册为安全上下文，可使用 `localStorage`、`crypto.subtle` 等仅限安全上下文的 Web API，且不需要监听本地端口。

### 原生菜单

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

`CmdOrCtrl` 在 macOS 为 `Cmd`，在其他平台为 `Ctrl`。`DefaultMenus` 在 macOS 返回应用菜单和 Edit 菜单，在 Linux/Windows 返回空切片。Chrome 后端目前忽略 `SetMenu`。

### UI 线程

`MainThread` 用于必须在平台 UI 线程中执行的窗口或系统 API：

```go
w.MainThread(func() {
	// macOS: AppKit 线程
	// Windows: Win32 消息循环线程
	// Linux: GTK 主线程
})
```

## 通用应用外壳 `cmd/app`

`cmd/app` 从当前目录的 `app.data` zip 文件加载应用；找不到 zip 时回退到 `data/` 目录。两种数据源使用相同结构：

```text
app.data（zip）或 data/
├── config.json
└── dist/
    ├── index.html
    └── ...
```

`config.json` 示例：

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

外壳向页面绑定了 `app.version()`、`app.close()` 和 `app.debug(message)`。完整实现见 [`cmd/app`](./cmd/app)。
