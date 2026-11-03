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
| `webview.Run() error` | 启动进程事件循环，直到显式调用 Quit；w.Run() 为兼容入口 |
| `webview.Quit() error` | 清理所有窗口和托盘并停止事件循环，不调用 os.Exit |
| `Show() error` / `Hidden() error` | 显示并激活 / 隐藏窗口，保留页面状态 |
| `OnClose(func()) error` | 拦截原生关闭请求；nil 恢复销毁行为 |
| `Close() error` | 销毁当前窗口及 WebView；已关闭后其他操作返回 ErrClosed |
| `SetTitle(title) error` | 设置标题 |
| `SetSize(width, height, hint) error` | 设置窗口尺寸约束 |
| `Navigate(url) error` | 导航到 URL |
| `SetHTML(html) error` | 加载 HTML 字符串 |
| `Eval(js) error` | 在当前页面执行 JavaScript |
| `Init(js) error` | 注册每次页面加载前执行的 JavaScript |
| `Bind(name, fn) error` | 将 Go 函数暴露给 JavaScript |
| `Unbind(name) error` | 删除已绑定的函数 |
| `SetMenu(menus...) error` | 替换原生菜单栏 |
| `MainThread(fn)` | 在 UI 线程执行函数并等待完成 |
| `InterceptResource(scheme, handler)` | 注册自定义 URL Scheme 处理器；应在 `Run` 前调用 |

`SetTitle`、`SetSize`、`Navigate`、`SetHTML` 和 `Init` 均可在 `Run` 前调用。

### Init 与 Eval 的执行时机

`Init(js)` 注册在**后续文档开始加载时**执行的脚本，可在 Run 前调用。运行中新增 Init 不会立即在当前页面执行，需要下一次导航或加载新文档才生效。此时 DOM 可能尚未构建，访问 document.body 等节点应放在 DOMContentLoaded 回调中。

`Eval(js)` 只在**当前页面**执行一次，不会保存到后续页面。应在窗口运行、页面就绪后调用；在 Run 前调用会返回错误，请改用 Init。SetHTML 和 Navigate 发起的是异步加载，紧接着调用 Eval 并不代表目标页面已经加载完毕。Eval 返回 nil 也不代表脚本的所有异步操作已经完成。

例如，通过页面就绪回调执行 Eval，避免使用固定时间的 Sleep 猜测加载时机：

```go
if err := w.Bind("pageReady", func() {
    if err := w.Eval(`document.body.style.backgroundColor = '#eef6ff'`); err != nil {
        fmt.Println("Eval:", err)
    }
}); err != nil {
    panic(err)
}
if err := w.Init(`
    document.addEventListener('DOMContentLoaded', () => {
        console.log('Init registered at document start');
        pageReady();
    }, {once: true});
`); err != nil {
    panic(err)
}
if err := w.SetHTML(`<!doctype html><html><body>Init / Eval demo</body></html>`); err != nil {
    panic(err)
}
if err := w.Run(); err != nil {
    panic(err)
}
```

### 文件与目录选择

文件对话框现在是包级函数，原来的 `w.ChooseDir()`、`w.ChooseFile()` 和 `w.SaveFile()` 已移除：

| 函数 | 行为 |
| --- | --- |
| `webview.ChooseDir()` | 选择一个目录 |
| `webview.ChooseFile(filters ...webview.FileFilter)` | 选择一个文件，可按扩展名和 MIME 类型过滤 |
| `webview.SaveFile(opts webview.FileDialogOptions)` | 选择保存路径，支持默认目录、建议文件名和原生覆盖确认 |

三个函数同步等待选择并返回绝对路径，无需创建 WebView 或托盘，可在 `Run` 前及运行期间使用，Chrome 后端也可使用。对话框不绑定业务窗口，关闭全部窗口后仍可使用。`SaveFile` 不创建文件，也不会自动运行或退出应用。

macOS 在 `Run` 前必须从 main 主 goroutine 调用，其他线程返回 `ErrDialogMainThread`；运行期间可从普通 goroutine、绑定函数或托盘回调调用。请求按入队顺序串行显示，并复制参数；等待期间调用方应避免修改正在传入的切片。UI 线程在已有请求时调用返回 `ErrDialogReentrant`。

取消返回 `ErrDialogCancelled`，缺少原生能力返回 `ErrDialogUnsupported`。`Quit` 会取消活动和排队请求，这些请求及退出后的新请求返回 `ErrApplicationStopped`。错误均可用 `errors.Is` 判断。

独立示例见 [`example/dialogs`](example/dialogs/main.go)：先在 `Run` 前选文件，再从纯托盘回调打开三个对话框。

```go
if err := w.Bind("chooseFile", func() (string, error) {
	path, err := webview.ChooseFile()
	if errors.Is(err, webview.ErrDialogCancelled) {
		return "", nil
	}
	return path, err
}); err != nil {
	log.Fatal(err)
}
```

可使用扩展名或 MIME 类型过滤文件：

```go
path, err := webview.ChooseFile(webview.FileFilter{
	Name: "Images", Extensions: []string{"png", "jpg"},
	MIMETypes: []string{"image/png", "image/jpeg"},
})
```

`SaveFile` 只弹出保存面板并返回绝对路径，由调用方负责实际写文件：

```go
path, err := webview.SaveFile(webview.FileDialogOptions{
	Title: "另存为", Filename: "report.pdf",
	Filters: []webview.FileFilter{{Name: "PDF", Extensions: []string{"pdf"}, MIMETypes: []string{"application/pdf"}}},
})
```

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
			Action:   func() { _ = webview.Quit() },
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

### 原生文件拖放

`OnFilesDropped` 接收拖入窗口内容区域的本地文件和目录（路径为绝对、清理后的路径）：

```go
if err := w.OnFilesDropped(func(paths []string) {
	for _, path := range paths {
		fmt.Println(path)
	}
}); err != nil {
	log.Fatal(err)
}
```

可在 `Run` 前或运行期间注册/替换。启用时原生层独占本地文件拖放，网页的 `dragover`/`drop` 不会收到该操作；回调异步且严格串行，不会阻塞 UI 线程。`paths` 保留拖入顺序和重复项，忽略 URL、文本、虚拟文件及没有本地路径的项目。使用 `w.OnFilesDropped(nil)` 注销并恢复浏览器默认行为。Chrome/Chromium 后端注册非空处理器会返回 `webview.ErrDropUnsupported`。

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

### 系统托盘与原生右键菜单

托盘无需 WebView，可创建多个。窗口、托盘和进程事件循环分别管理生命周期：关闭最后一个窗口或托盘仍会保持运行，只有 `webview.Quit()` 才清理全部对象并让 `Run()` 返回。`w.Run()` 是包级 `Run()` 的兼容入口。

```go
w, err := webview.New(webview.Options{})
if err != nil { panic(err) }
if err := w.SetHTML(`<input value="隐藏后仍保留">`); err != nil { panic(err) }
if err := w.OnClose(func() {
    if err := w.Hidden(); err != nil { log.Print(err) }
}); err != nil { panic(err) }
tray, err := webview.NewSystray(webview.SystrayOptions{
    Icon: pngBytes,
    Title: "运行中",
    Tooltip: "点击显示窗口",
    OnClick: func() { if err := w.Show(); err != nil { log.Print(err) } },
    Items: []webview.MenuItem{
        {ID: "show", Label: "显示窗口", Action: func() {
            if err := w.Show(); err != nil { log.Print(err) }
        }},
        {ID: "quit", Label: "退出", Action: func() {
            if err := webview.Quit(); err != nil { log.Print(err) }
        }},
    },
})
if err != nil { panic(err) }
_ = tray // tray.Update(opts) 整体更新；tray.Close() 幂等移除。
if err := webview.Run(); err != nil { panic(err) }
```

旧 `w.SetSystray` / `w.RemoveSystray` 已移除，迁移为 `NewSystray`、`tray.Update` 和 `tray.Close`。菜单中的“退出应用”应调用 `webview.Quit()`。`Title` 仅在 macOS 显示，可用纯文字代替 Icon；Windows、Linux 需要 PNG 图标。`Tooltip` 是独立的悬停提示。

`Show` 显示并激活窗口；`Hidden` 保留 DOM、输入、页面及 JS 状态，不重复执行 Init。隐藏不代表操作系统挂起，后台 JS 节流仍遵循引擎和 Offscreen 配置。`Close` 真正销毁当前窗口与 WebView，不能用 Show 重建，必须重新 New。关闭窗口不会影响其他窗口或托盘。

`OnClose` 接管关闭按钮等原生请求，回调自行选择 Hidden、Close 或不处理。同一窗口回调运行时的重复请求会合并，传 nil 恢复默认销毁。主动 Close 和 Quit 绕过 OnClose。回调异步执行，Quit 不等待正在执行的用户回调；关闭对象会使其尚未执行的回调失效。

Run 前创建的对象只保存配置，Hidden 可设置初始隐藏；运行中 New / NewSystray 会等待原生创建结果。可在 Run 前 Close 待创建对象。启动环境错误由 Run 返回并清理资源。并发 Run 返回 ErrAlreadyRunning；Quit 后禁止创建对象或再次运行，返回 ErrApplicationStopped。macOS 的 Run 应在 main 函数的主 goroutine 调用，以满足 AppKit 主线程要求。同一进程只能启动一次应用事件循环。无窗口和托盘时 Run 也会等待 Quit。

Chrome 的 Close 只结束对应浏览器进程，Show / Hidden / OnClose 返回 ErrWindowUnsupported；独立原生托盘不受窗口后端选择影响。

完整示例：[`example/systray`](example/systray/main.go) 演示关闭按钮隐藏、点击托盘恢复；[`example/tray-only`](example/tray-only/main.go) 演示纯托盘启动，以及窗口关闭后继续创建新窗口；[`example/native-menus`](example/native-menus/main.go) 演示托盘更新与右键菜单。

OnClick 自定义左键行为，未设置时使用菜单；右键仍打开菜单。Linux 依赖宿主通过 StatusNotifierItem.Activate 通知点击：部分桌面始终显示菜单或忽略 Activate，具体行为取决于宿主。每个 Linux 托盘拥有独立的 D-Bus 连接，无需 GTK/WebView 窗口。

#### 右键菜单接口

| 方法 | 行为 |
| --- | --- |
| `SetContextMenu([]webview.MenuItem)` | 完全替换默认右键菜单和之前的自定义项，仅显示传入的菜单项；同时清除之前按 role 移除的配置 |
| `AddContextMenuItem(webview.MenuItem)` | 追加一个自定义项；未调用 SetContextMenu 时保留浏览器默认项，调用后则追加到替换后的菜单 |
| `RemoveContextMenuItem(id string)` | 按 ID 移除自定义项；不存在的 ID 无副作用 |
| `RemoveContextMenuItemRole(...webview.MenuItemRole)` | 按标准编辑角色隐藏默认项，并移除具有相同 Role 的自定义项；支持一次传入多个 role |

`SetContextMenu(nil)` 或空切片表示使用空菜单，禁用右键菜单，**不会恢复默认菜单**。删除替换菜单中的最后一项后仍为空菜单，也不会自动恢复默认项。自定义项（包括分隔符）必须有非空且唯一的 ID；重复追加 ID 会返回错误。菜单会自动清理首尾及连续分隔符。

完全替换默认右键菜单，然后追加、移除自定义项：

```go
if err := w.SetContextMenu([]webview.MenuItem{
    {
        ID: "hello",
        Label: "自定义动作",
        Action: func() { w.Eval(`alert('Hello from Go')`) },
    },
}); err != nil {
    panic(err)
}
if err := w.AddContextMenuItem(webview.MenuItem{
    ID: "extra",
    Label: "追加项",
    Action: func() { w.Eval(`alert('Extra')`) },
}); err != nil {
    panic(err)
}
if err := w.RemoveContextMenuItem("extra"); err != nil {
    panic(err)
}
```

#### 按 role 移除菜单项

预定义 role 如下。匹配使用原生动作标识，不依赖菜单显示语言；当前菜单中不存在的动作会忽略，其他默认项保持原生行为。

| 预定义 role | 动作 |
| --- | --- |
| `webview.MenuItemRoleUndo` | 撤销 |
| `webview.MenuItemRoleRedo` | 重做 |
| `webview.MenuItemRoleCut` | 剪切 |
| `webview.MenuItemRoleCopy` | 复制 |
| `webview.MenuItemRolePaste` | 粘贴 |
| `webview.MenuItemRoleSelectAll` | 全选 |
| `webview.MenuItemRoleNone` | 无标准动作（MenuItem.Role 的零值），不能传给 RemoveContextMenuItemRole，否则返回错误 |

例如，保留浏览器默认菜单，隐藏“复制”和“剪切”，并追加 Go 回调项。此场景直接使用 Add，**不要先调用 SetContextMenu**：

```go
if err := w.RemoveContextMenuItemRole(
    webview.MenuItemRoleCopy,
    webview.MenuItemRoleCut,
); err != nil {
    panic(err)
}
if err := w.AddContextMenuItem(webview.MenuItem{
    ID: "hello",
    Label: "自定义动作",
    Action: func() { w.Eval(`alert('Hello from Go')`) },
}); err != nil {
    panic(err)
}
```

自定义项可通过 `Role: webview.MenuItemRoleCopy` 等标记编辑角色；RemoveContextMenuItemRole 会连同这些匹配的自定义项一起移除，未标记 Role 的自定义项继续保留。重复移除同一 role 无额外副作用，传入未知 role 返回错误。

可在 Run 前配置，也可在运行时更新。回调异步执行，可调用菜单更新、Eval 和 Close；多个回调可能并发，应用共享状态应自行同步。菜单栏、托盘、右键菜单独立管理回调。窗口关闭时只释放所属窗口资源，托盘及进程事件循环继续运行。

macOS 使用 NSStatusItem 和原生菜单打开钩子；Windows 使用通知图标及 WebView2 ContextMenuRequested，需要支持该接口的运行时，支持 Explorer 重启后恢复图标；Linux 使用 WebKitGTK 6.0、会话 D-Bus 的 StatusNotifierItem 与 DBusMenu，需要桌面提供 StatusNotifier 托盘宿主。Chrome 的窗口右键菜单或缺失原生能力时返回可用 `errors.Is(err, webview.ErrMenuUnsupported)` 判断的错误。Run 前无法确定的运行时或桌面初始化失败由 Run 返回。
