# purego-webview

无 CGO 的 Go 跨平台 Webview 框架，基于 [purego](https://github.com/ebitengine/purego)。

[English](./README.en.md)

## 特性

- **零 CGO** -- 像普通 Go 项目一样交叉编译，无需 C 工具链
- **macOS** -- WKWebView + AppKit，通过 purego 调用 ObjC 运行时
- **Windows** -- WebView2，通过纯 COM 互操作 (syscall.SyscallN)
- **完整 JS 桥接** -- 从 JavaScript 调用 Go 函数，通过 Promise 返回结果
- **预 Run 缓冲** -- `SetTitle`、`SetHTML`、`Navigate` 可在 `Run()` 前调用
- **内嵌 WebView2Loader.dll** -- 按架构 (amd64/arm64/x86)，自动解压到临时目录
- **原生文件选择器** -- macOS 上 `<input type=file>` 映射到 NSOpenPanel
- **隐身模式** -- 内存数据存储，不持久化 cookie/缓存

## 平台状态

| 平台 | 后端 | 状态 |
|------|------|------|
| macOS | WKWebView + AppKit (purego) | 可用 |
| Windows | WebView2 (COM 互操作) | 可用 |
| Linux | WebKitGTK | 计划中 |

## 环境要求

- Go 1.24+
- macOS 10.13+ 或 Windows 10+（需安装 [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/)）

## 安装

```bash
go get github.com/opentoys/webview
```

## 快速开始

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

### 创建

```go
func New(opts Options) (*W, error)
```

创建 webview 窗口，返回句柄 `*W`。

```go
w, err := webview.New(webview.Options{
    Debug:     true,     // 启用开发者工具
    Incognito: true,     // 内存数据存储
    DataDir:   "./data", // 自定义数据目录（Windows 默认 %AppData%\<exe>）
})
```

### 控制

| 方法 | 签名 | 说明 |
|------|------|------|
| `Run` | `func (w *W) Run() error` | 启动事件循环，阻塞直到窗口关闭 |
| `Close` | `func (w *W) Close() error` | 关闭窗口 |
| `SetTitle` | `func (w *W) SetTitle(title string)` | 设置窗口标题 |
| `SetSize` | `func (w *W) SetSize(w, h int, hint SizeHint)` | 设置窗口尺寸 |
| `Navigate` | `func (w *W) Navigate(url string) error` | 导航到 URL |
| `SetHTML` | `func (w *W) SetHTML(html string) error` | 加载 HTML 字符串 |
| `Eval` | `func (w *W) Eval(js string) error` | 执行 JavaScript |

### SizeHint

| 常量 | 值 | 说明 |
|------|-----|------|
| `SizeNone` | 0 | 无约束 |
| `SizeMin` | 1 | 最小尺寸 |
| `SizeMax` | 2 | 最大尺寸 |
| `SizeFixed` | 3 | 固定尺寸 |

### Bind（JS 桥接）

```go
func (w *W) Bind(name string, fn any) error
```

将 Go 函数暴露给 JavaScript。函数在 webview 中成为全局的、返回 Promise 的函数。

**支持的返回签名：**
- `func(args...)` -- 无返回值，Promise resolve 为 `undefined`
- `func(args...) T` -- 返回值，Promise resolve 为 `T`
- `func(args...) (T, error)` -- 返回值或错误，错误时 Promise reject
- `func(args...) error` -- 仅返回错误

**参数类型：** 任何可 JSON 序列化的 Go 类型。JS 参数通过 `encoding/json` 解码。

```go
// 无返回
w.Bind("log", func(msg string) {
    fmt.Println(msg)
})

// 返回值
w.Bind("add", func(a, b int) int {
    return a + b
})

// 返回值 + 错误
w.Bind("readFile", func(path string) (string, error) {
    data, err := os.ReadFile(path)
    return string(data), err
})
```

JavaScript 端：

```javascript
// 所有绑定函数返回 Promise
await log("hello from JS");
const sum = await add(1, 2);
try {
    const content = await readFile("/etc/hosts");
} catch (e) {
    console.error(e.message);
}
```

## JS 桥接协议

桥接使用 JSON 消息协议在 JS 和 Go 之间通信。

**JS -> Go（请求）：**
```json
{"id": 1, "name": "add", "args": [1, 2]}
```

**Go -> JS（响应）：**
```javascript
webviewBridge.resolve(1, 3)       // 成功
webviewBridge.reject(1, "error")  // 失败
```

传输层：
- macOS: `window.webkit.messageHandlers.webviewBridge.postMessage()`
- Windows: `window.chrome.webview.postMessage()`

## 构建与运行

```bash
# macOS
CGO_ENABLED=0 go run ./example

# Windows（从 macOS/Linux 交叉编译）
GOOS=windows GOARCH=amd64 go build -o app.exe ./example

# 或在 Windows 上原生构建
go build -o app.exe ./example
```

## 测试

```bash
CGO_ENABLED=0 go test ./...
```

## Windows WebView2 加载器

WebView2 需要 `WebView2Loader.dll` 来引导启动。库会自动处理：

**搜索顺序：**
1. `X_WEBVIEW2LOADER_DLL` 环境变量（显式路径）
2. 内嵌 DLL（按架构，解压到临时目录，基于哈希缓存）
3. 系统 DLL（搜索 PATH 和可执行文件目录）
4. 可执行文件目录显式搜索

**内嵌架构：** amd64、arm64、x86。其他架构回退到系统 DLL。

如未安装 WebView2 Runtime，请从 [Microsoft](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) 下载。

## 示例

### 计数器

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

### 导航到 URL

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

### 隐身模式 + 自定义数据目录

```go
w, _ := webview.New(webview.Options{
    Incognito: true,              // 不持久化存储
    DataDir:   "./my-app-data",   // 自定义数据目录
})
```

## License

MIT
