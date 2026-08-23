# purego-webview

无 CGO 的 Go 跨平台 Webview 框架，基于 [purego](https://github.com/ebitengine/purego)。

[English](./README.en.md)

## 特性

- **零 CGO** -- 像普通 Go 项目一样交叉编译，无需 C 工具链
- **三端支持** -- macOS (WKWebView) / Windows (WebView2) / Linux (WebKitGTK)
- **完整 JS 桥接** -- 从 JavaScript 调用 Go 函数，通过 Promise 返回结果
- **原生菜单** -- 三平台原生菜单栏，支持快捷键和回调
- **自定义 URL Scheme** -- `app://` 等自定义协议，安全上下文，无需开端口
- **JS 注入** -- `Init(js)` 在每次页面加载前执行 JS
- **预 Run 缓冲** -- `SetTitle`、`SetHTML`、`Navigate` 可在 `Run()` 前调用
- **原生文件选择器** -- `<input type=file>` 映射到系统文件选择器，支持 `accept` 过滤
- **隐身模式** -- 内存数据存储，不持久化 cookie/缓存
- **内嵌 WebView2Loader.dll** -- 按架构 (amd64/arm64/x86)，自动解压到临时目录

## 平台状态

| 平台 | 后端 | 状态 |
|------|------|------|
| macOS | WKWebView + AppKit (purego) | 可用 |
| Windows | WebView2 (COM 互操作) | 可用 |
| Linux | WebKitGTK (purego) | 可用 |

## 环境要求

- Go 1.25+
- macOS 10.13+
- Windows 10+（需安装 [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/)）
- Linux：需安装 WebKitGTK：

```bash
# Debian / Ubuntu
apt install libwebkit2gtk-4.1-0

# Fedora
dnf install webkit2gtk4.1

# Arch
pacman -S webkit2gtk-4.1
```

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
w, err := webview.New(webview.Options{
    Debug:     true,     // 启用开发者工具
    Incognito: true,     // 内存数据存储
    DataDir:   "./data", // 自定义数据目录（Windows 默认 %AppData%\<exe>）
})
```

### 控制

| 方法 | 说明 |
|------|------|
| `Run()` | 启动事件循环，阻塞直到窗口关闭 |
| `Close()` | 关闭窗口 |
| `SetTitle(title)` | 设置窗口标题 |
| `SetSize(w, h, hint)` | 设置窗口尺寸 |
| `Navigate(url)` | 导航到 URL |
| `SetHTML(html)` | 加载 HTML 字符串 |
| `Eval(js)` | 执行 JavaScript |
| `Init(js)` | 注入每次页面加载前执行的 JS |
| `Bind(name, fn)` | 将 Go 函数暴露给 JS |
| `Unbind(name)` | 移除已绑定的 JS 函数 |
| `SetMenu(menus...)` | 设置原生菜单栏 |
| `InterceptResource(scheme, handler)` | 注册自定义 URL scheme 资源拦截 |

### SizeHint

| 常量 | 值 | 说明 |
|------|-----|------|
| `SizeNone` | 0 | 无约束 |
| `SizeMin` | 1 | 最小尺寸 |
| `SizeMax` | 2 | 最大尺寸 |
| `SizeFixed` | 3 | 固定尺寸 |

### 原生菜单

```go
// 平台修饰键：macOS 为 "Cmd"，其他为 "Ctrl"
webview.CmdOrCtrl

// 获取平台默认菜单（macOS 返回 Edit 菜单，其他返回空）
menus := webview.DefaultMenus(w)

// 添加自定义菜单
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

**类型定义：**

```go
type Menu struct {
    Label string
    Items []MenuItem
}

type MenuItem struct {
    Label     string
    Shortcut  string // "Ctrl+Z", "Cmd+Shift+Z" 等
    Action    func()
    Separator bool   // 为 true 时忽略其他字段
}
```

### Bind（JS 桥接）

将 Go 函数暴露给 JavaScript。函数在 webview 中成为全局的、返回 Promise 的函数。

**支持的返回签名：**
- `func(args...)` -- 无返回值，Promise resolve 为 `undefined`
- `func(args...) T` -- 返回值，Promise resolve 为 `T`
- `func(args...) (T, error)` -- 返回值或错误，错误时 Promise reject
- `func(args...) error` -- 仅返回错误

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

### InterceptResource（自定义 URL Scheme）

注册自定义 URL scheme 的资源拦截器。必须在 `Run()` 前调用。

`app://` 等自定义协议在所有平台上都被视为**安全上下文**（`localStorage`、`crypto.subtle`、`getUserMedia` 等均可用），且无需开端口。

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

**类型定义：**

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
```

### 文件选择器（macOS）

macOS 上 `<input type=file>` 的 `accept` 属性原生支持文件类型过滤：

```html
<input type="file" accept="image/png,application/pdf">  <!-- MIME 类型 -->
<input type="file" accept=".png,.jpg,.pdf">              <!-- 扩展名 -->
<input type="file" accept="image/*,video/*">             <!-- 通配符 -->
```

## cmd/app -- 通用 App 启动外壳

`cmd/app` 是一个通用的桌面应用启动器，从 zip 文件或目录加载前端资源 + 配置。

**目录结构：**
```
app.data (zip) 或 data/
├── config.json
└── dist/
    └── index.html
```

**config.json：**
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

**运行：**
```bash
go run ./cmd/app              # 从 data/ 目录
CGO_ENABLED=0 go build -o myapp ./cmd/app  # 构建
```

**JS 桥接函数：** `app.version()` / `app.close()` / `app.debug(msg)`

## 构建与运行

```bash
CGO_ENABLED=0 go run ./example                    # macOS / Linux
GOOS=windows GOARCH=amd64 go build -o app.exe ./example  # 交叉编译 Windows
```

## 测试

```bash
CGO_ENABLED=0 go test ./...
```

## License

MIT
