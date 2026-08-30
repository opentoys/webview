// Package chrome implements a webview backend that drives Chrome/Chromium over
// the DevTools Protocol (CDP) via --remote-debugging-pipe. It replaces the
// WebSocket transport used by the original lorca client with direct pipe I/O
// (child fd3 = commands in, child fd4 = events out), so no third-party WebSocket
// dependency is needed.
//
// The backend satisfies the webview.Platform interface structurally; webview.New
// selects it when Options.Backend == BackendChrome ("chrome"), or — at
// construction time — when BackendFallbackWebview finds a Chrome/Chromium
// executable.
package chrome

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/opentoys/webview/internal/types"
)

// h is a shorthand for a JSON object map used in CDP params.
type h = map[string]any

type result struct {
	Value json.RawMessage
	Err   error
}

// cdpMsg is the wire shape of a CDP message (command, event, or response).
type cdpMsg struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Options configures the Chrome backend.
type Options struct {
	Debug     bool
	Incognito bool
	DataDir   string
	// Executable overrides ChromeExecutable() when non-empty.
	Executable string
}

// Chrome is a CDP-backed webview platform.
type Chrome struct {
	sync.Mutex
	wmu      sync.Mutex
	cmd      *exec.Cmd
	in       *os.File // write end (Chrome reads commands from here)
	out      *os.File // read end (Chrome writes events here)
	id       int32
	pending  map[int]chan result
	done     chan struct{}
	attached chan struct{}
	once     sync.Once

	// sessionID is the auto-attached page target's CDP session. With
	// flatten=true, page-domain commands must carry this sessionId; the root
	// session only handles Target.*/Browser.* domains.
	sessionID string

	schemeHandlers map[string]types.ResourceHandler
	tmpDir         string
	stderrLog      *os.File

	Debug      bool
	Incognito  bool
	DataDir    string
	Executable string

	// Set before Run; baked into the --app launch URL so Chrome opens the
	// real content directly instead of a post-boot round-trip.
	startURL     string
	startHTML    string
	pendingTitle string
	started      bool

	// deferredURL holds a pre-Run scheme URL (app://...) that must NOT be
	// baked into --app: Chrome would issue it before Fetch.enable is ready
	// and flash an error page. We launch about:blank instead and navigate
	// to it after Fetch.enable.
	deferredURL string

	// Wired by webview.buildChrome before Run.
	BoundFuncs  func() []string
	MessageFunc func(string)
}

// defaultChromeArgs are the stability/headless-friendly flags shared by all
// Chrome invocations.
var defaultChromeArgs = []string{
	"--disable-background-networking",
	"--disable-background-timer-throttling",
	"--disable-backgrounding-occluded-windows",
	"--disable-breakpad",
	"--disable-client-side-phishing-detection",
	"--disable-default-apps",
	"--disable-dev-shm-usage",
	"--disable-extensions",
	"--disable-features=site-per-process",
	"--disable-hang-monitor",
	"--disable-ipc-flooding-protection",
	"--disable-popup-blocking",
	"--disable-prompt-on-repost",
	"--disable-renderer-backgrounding",
	"--disable-sync",
	"--disable-translate",
	"--metrics-recording-only",
	"--no-first-run",
	"--no-default-browser-check",
	"--password-store=basic",
	"--use-mock-keychain",
}

// New creates a Chrome backend. Chrome is launched lazily in Run.
func New(opts Options) *Chrome {
	return &Chrome{
		id:         1,
		pending:    map[int]chan result{},
		done:       make(chan struct{}),
		attached:   make(chan struct{}),
		Debug:      opts.Debug,
		Incognito:  opts.Incognito,
		DataDir:    opts.DataDir,
		Executable: opts.Executable,
	}
}

var errClosed = errors.New("webview/chrome: connection closed")

// --- lifecycle -------------------------------------------------------------

// Run launches Chrome and blocks until the window is closed.
func (c *Chrome) Run() error {
	if err := c.start(); err != nil {
		return err
	}
	c.started = true
	go c.readLoop()

	// Auto-attach to the app target so we can speak CDP without session wrapping.
	c.send("Target.setAutoAttach", h{
		"autoAttach":             true,
		"waitForDebuggerOnStart": false,
		"flatten":                true,
	})
	select {
	case <-c.attached:
	case <-time.After(5 * time.Second):
	}

	c.send("Runtime.enable", nil)
	c.send("Page.enable", nil)
	c.Lock()
	if len(c.schemeHandlers) > 0 {
		c.Unlock()
		c.updateFetch()
	} else {
		c.Unlock()
	}
	// Pre-Run scheme URLs (app://...) were not baked into --app (that would
	// race Fetch.enable and flash an error page). Navigate now, with Fetch
	// live, so the request is intercepted.
	if c.deferredURL != "" {
		c.send("Page.navigate", h{"url": c.deferredURL})
	}
	c.send("Runtime.addBinding", h{"name": "webviewBridge"})
	c.send("Page.addScriptToEvaluateOnNewDocument", h{"source": bootstrapJS()})
	// The initial --app document is created before Run() connects, so
	// addScriptToEvaluateOnNewDocument (which fires only on new documents)
	// won't cover it. Eval the bootstrap directly on the live page now; its
	// installer polls until the native binding appears, then wraps it.
	c.Eval(bootstrapJS())
	c.injectBridge()

	return c.cmd.Wait()
}

func (c *Chrome) start() error {
	bin := c.Executable
	if bin == "" {
		bin = ChromeExecutable()
	}
	if bin == "" {
		return errors.New("webview/chrome: no Chrome/Chromium executable found (set WEBVIEW_CHROME)")
	}

	// Pipe pair 1: w3 -> child fd3 (Chrome reads commands).
	// Pipe pair 2: r4 <- child fd4 (Chrome writes events).
	r3, w3, err := os.Pipe()
	if err != nil {
		return err
	}
	r4, w4, err := os.Pipe()
	if err != nil {
		return err
	}

	dir := c.DataDir
	if dir == "" {
		dir, err = os.MkdirTemp("", "webview-chrome")
		if err != nil {
			return err
		}
		c.tmpDir = dir
	}

	// Pre-Run Navigate/SetHTML are baked into the --app URL so Chrome opens
	// the real content at launch. SetHTML content goes to a data-dir file
	// (Chrome rejects a data: URL in --app mode).
	appURL := c.startURL
	if appURL == "" {
		if c.startHTML != "" {
			htmlPath := filepath.Join(dir, "webview-start.html")
			if err := os.WriteFile(htmlPath, []byte(c.startHTML), 0o600); err != nil {
				return err
			}
			appURL = "file://" + htmlPath
		} else {
			appURL = "data:text/html," + url.PathEscape(fmt.Sprintf(`<title>%s</title>`, c.pendingTitle))
		}
	}

	cmd := exec.Command(bin, c.buildArgs(dir, appURL)...)
	// Chrome's --remote-debugging-pipe convention (devtools_pipe_handler.cc):
	//   child fd3 = INPUT  (Chrome reads CDP commands from here)
	//   child fd4 = OUTPUT (Chrome writes CDP events here)
	// r3/w3 is the command pipe, r4/w4 the event pipe. So fd3 gets the
	// command read-end (r3) and fd4 gets the event write-end (w4).
	cmd.ExtraFiles = []*os.File{r3, w4}
	if c.Debug {
		// Keep Chrome's own noise off the same fd our pipe logs use so the
		// wire trace stays readable. The file is removed on Close.
		f, err := os.CreateTemp("", "webview-chrome-stderr-*.log")
		if err == nil {
			cmd.Stderr = f
			c.stderrLog = f
		} else {
			cmd.Stderr = io.Discard
		}
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Child now owns fd3=r3 and fd4=w4. Parent keeps w3 (write commands)
	// and r4 (read events); close the ends handed to the child.
	r3.Close()
	w4.Close()

	c.cmd = cmd
	c.in = w3  // parent writes commands → Chrome reads fd3
	c.out = r4 // Chrome writes fd4 → parent reads events
	return nil
}

func (c *Chrome) buildArgs(dir, appURL string) []string {
	args := append([]string{}, defaultChromeArgs...)
	args = append(args, "--remote-debugging-pipe")
	// at launch (baked-in URL) instead of navigating post-boot.
	args = append(args, "--app="+appURL)
	if c.Incognito {
		args = append(args, "--incognito")
	}
	args = append(args, "--user-data-dir="+dir)
	args = append(args, "--window-size=800,600")
	return args
}

// Close kills the Chrome process. Run returns once it exits.
func (c *Chrome) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	if c.tmpDir != "" {
		_ = os.RemoveAll(c.tmpDir)
	}
	if c.stderrLog != nil {
		_ = c.stderrLog.Close()
		c.stderrLog = nil
	}
	return nil
}

// --- transport -------------------------------------------------------------

func (c *Chrome) writeMsg(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if c.Debug {
		fmt.Fprintln(os.Stderr, "→pipe:", string(b))
	}
	// Chrome's --remote-debugging-pipe framing: each message is a single JSON
	// value followed by a NUL byte (not length-prefixed).
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.in.Write(b); err != nil {
		return err
	}
	_, err = c.in.Write([]byte{0})
	return err
}

func (c *Chrome) send(method string, params h) (json.RawMessage, error) {
	id := atomic.AddInt32(&c.id, 1)
	resc := make(chan result, 1)
	c.Lock()
	c.pending[int(id)] = resc
	c.Unlock()

	msg := h{"id": int(id), "method": method, "params": params}
	if c.sessionID != "" {
		msg["sessionId"] = c.sessionID
	}
	if err := c.writeMsg(msg); err != nil {
		c.Lock()
		delete(c.pending, int(id))
		c.Unlock()
		return nil, err
	}

	select {
	case res := <-resc:
		return res.Value, res.Err
	case <-c.done:
		return nil, errClosed
	}
}

func (c *Chrome) readLoop() {
	defer close(c.done)
	r := bufio.NewReader(c.out)
	for {
		// Chrome frames each CDP message as a JSON value followed by a NUL
		// byte. Read up to the NUL delimiter.
		buf, err := r.ReadBytes(0)
		if err != nil {
			if c.Debug {
				fmt.Fprintln(os.Stderr, "←pipe read err:", err)
			}
			return
		}
		buf = buf[:len(buf)-1] // drop trailing NUL
		if len(buf) == 0 {
			continue
		}
		if c.Debug {
			fmt.Fprintln(os.Stderr, "←pipe:", string(buf))
		}
		var m cdpMsg
		if err := json.Unmarshal(buf, &m); err != nil {
			continue
		}
		c.dispatch(m)
	}
}

func (c *Chrome) dispatch(m cdpMsg) {
	if m.ID != 0 {
		c.Lock()
		ch, ok := c.pending[m.ID]
		delete(c.pending, m.ID)
		c.Unlock()
		if ok {
			var err error
			if m.Error != nil {
				err = errors.New(m.Error.Message)
			}
			ch <- result{Value: m.Result, Err: err}
		}
		return
	}

	switch m.Method {
	case "Target.attachedToTarget":
		var p struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				Type string `json:"type"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(m.Params, &p) == nil && p.TargetInfo.Type == "page" {
			c.sessionID = p.SessionID
			c.once.Do(func() { close(c.attached) })
		}
	case "Runtime.bindingCalled":
		var p struct {
			Payload string `json:"payload"`
		}
		if json.Unmarshal(m.Params, &p) == nil && c.MessageFunc != nil {
			// Run off the read loop so binding callbacks may themselves call Eval.
			go c.MessageFunc(p.Payload)
		}
	case "Runtime.consoleAPICalled", "Runtime.exceptionThrown":
		if c.Debug {
			fmt.Fprintln(os.Stderr, "webview/chrome:", string(m.Params))
		}
	case "Page.frameNavigated":
		// Re-inject per-name stubs after each top-level navigation: the page
		// object is replaced, so the bootstrap's transport object survives
		// (addScriptToEvaluateOnNewDocument) but window.<name> stubs must return.
		go c.injectBridge()
	case "Fetch.requestPaused":
		go c.handleFetch(m.Params)
	case "Target.targetDestroyed", "Target.detachedFromTarget":
		// App window/page closed: terminate the Chrome process. Run
		// returns once cmd.Wait() unblocks after the kill.
		go c.Close()
	}
}

// --- Platform interface ----------------------------------------------------

func (c *Chrome) Eval(js string) error {
	_, err := c.send("Runtime.evaluate", h{
		"expression":    js,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	return err
}

// EvalHost runs JS from a binding reply without blocking the read loop.
func (c *Chrome) EvalHost(js string) {
	go c.Eval(js)
}

func (c *Chrome) Init(js string) error {
	_, err := c.send("Page.addScriptToEvaluateOnNewDocument", h{"source": js})
	return err
}

func (c *Chrome) Navigate(url string) error {
	if !c.started {
		// Don't bake a custom-scheme URL into --app: Chrome would request it
		// before Fetch.enable is registered and flash an error page. Capture
		// it for a post-Fetch Page.navigate instead. Non-scheme URLs still
		// bake so Chrome opens real content directly.
		if strings.Contains(c.rewriteSchemeURL(url), ".localhost/") {
			c.deferredURL = c.rewriteSchemeURL(url)
		} else {
			c.startURL = url
		}
		return nil
	}
	_, err := c.send("Page.navigate", h{"url": c.rewriteSchemeURL(url)})
	return err
}

func (c *Chrome) SetHTML(html string) error {
	if !c.started {
		// Baked into a data-dir file and loaded via --app at launch.
		c.startHTML = html
		return nil
	}
	expr := "document.open();document.write(" + jsString(html) + ");document.close();"
	return c.Eval(expr)
}

func (c *Chrome) SetTitle(title string) error {
	c.pendingTitle = title
	return c.Eval("document.title = " + jsString(title))
}

func (c *Chrome) SetSize(width, height int, hint types.SizeHint) {
	res, err := c.send("Browser.getWindowForTarget", nil)
	if err != nil {
		return
	}
	var win struct {
		WindowID int `json:"windowId"`
	}
	if err := json.Unmarshal(res, &win); err != nil {
		return
	}
	state := "normal"
	switch hint {
	case types.SizeMax:
		state = "maximized"
	case types.SizeMin:
		state = "minimized"
	}
	c.send("Browser.setWindowBounds", h{
		"windowId": win.WindowID,
		"bounds":   h{"width": width, "height": height, "windowState": state},
	})
}

func (c *Chrome) SetMenus(menus []types.Menu) {
	// Chrome app windows have no native menu bar; intentionally a no-op.
}

func (c *Chrome) MainThread(f func()) {
	// Chrome owns its own UI thread; our side is free-threaded, so run directly.
	f()
}

func (c *Chrome) InterceptResource(scheme string, handler types.ResourceHandler) {
	c.Lock()
	if c.schemeHandlers == nil {
		c.schemeHandlers = map[string]types.ResourceHandler{}
	}
	c.schemeHandlers[scheme] = handler
	c.Unlock()
	c.updateFetch()
}

func (c *Chrome) updateFetch() {
	c.Lock()
	patterns := make([]h, 0, len(c.schemeHandlers))
	for s := range c.schemeHandlers {
		// Mirror the Windows backend: a custom scheme like app://host/path is
		// rewritten to https://app.localhost/path, the reserved .localhost
		// TLD being a secure context Chrome intercepts via Fetch before any
		// network/cert handshake. Match the rewritten form here.
		patterns = append(patterns, h{
			"urlPattern":   "https://" + s + ".localhost/*",
			"resourceType": "",
			"requestStage": "Request",
		})
	}
	c.Unlock()
	c.send("Fetch.enable", h{"patterns": patterns})
}

func (c *Chrome) handleFetch(params json.RawMessage) {
	var p struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL      string            `json:"url"`
			Method   string            `json:"method"`
			Headers  map[string]string `json:"headers"`
			PostData string            `json:"postData"`
		} `json:"request"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}

	c.Lock()
	hnd, ok := c.schemeHandlers[schemeOf(p.Request.URL)]
	c.Unlock()
	if !ok {
		c.send("Fetch.continueRequest", h{"requestId": p.RequestID})
		return
	}

	req := types.ResourceRequest{
		URL:     p.Request.URL,
		Method:  p.Request.Method,
		Headers: http.Header{},
		Body:    []byte(p.Request.PostData),
	}
	for k, v := range p.Request.Headers {
		req.Headers.Set(k, v)
	}

	respond := func(resp *types.ResourceResponse) {
		if resp == nil {
			c.send("Fetch.continueRequest", h{"requestId": p.RequestID})
			return
		}
		hdrs := make([]h, 0, len(resp.Headers))
		for k, vs := range resp.Headers {
			for _, v := range vs {
				hdrs = append(hdrs, h{"name": k, "value": v})
			}
		}
		c.send("Fetch.fulfillRequest", h{
			"requestId":       p.RequestID,
			"responseCode":    resp.StatusCode,
			"responseHeaders": hdrs,
			"body":            base64.StdEncoding.EncodeToString(resp.Body),
		})
	}
	hnd(req, respond)
}

// --- helpers ---------------------------------------------------------------

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func schemeOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if idx := strings.Index(u.Hostname(), "."); idx > 0 {
		// https://app.localhost/path → "app"
		return u.Hostname()[:idx]
	}
	return u.Scheme
}

// rewriteSchemeURL converts scheme://host/path to https://scheme.localhost/path
// for registered schemes, so Fetch.enable (registered with the *.localhost
// pattern) intercepts them as a secure context without opening a TCP port.
// Unregistered URLs pass through unchanged.
func (c *Chrome) rewriteSchemeURL(rawURL string) string {
	c.Lock()
	_, ok := c.schemeHandlers[schemeOf(rawURL)]
	c.Unlock()
	if !ok {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	out := "https://" + u.Scheme + ".localhost/" + strings.TrimPrefix(u.Path, "/")
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.Fragment
	}
	return out
}
