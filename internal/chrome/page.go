package chrome

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/opentoys/webview/internal/debuglog"
	"github.com/opentoys/webview/internal/types"
)

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
		c.Logger.Log(BackendName, "navigate", map[string]string{"url": debuglog.URL(url), "phase": "queued"})
		return nil
	}
	_, err := c.send("Page.navigate", h{"url": c.rewriteSchemeURL(url)})
	fields := map[string]string{"url": debuglog.URL(url), "phase": "started"}
	if err != nil {
		fields["phase"] = "failed"
		fields["error"] = debuglog.Error(err)
	}
	c.Logger.Log(BackendName, "navigate", fields)
	return err
}

func (c *Chrome) SetHTML(html string) error {
	if !c.started {
		// Baked into a data-dir file and loaded via --app at launch.
		c.startHTML = html
		c.Logger.Log(BackendName, "load_html", map[string]string{"bytes": strconv.Itoa(len(html)), "phase": "queued"})
		return nil
	}
	expr := "document.open();document.write(" + jsString(html) + ");document.close();"
	err := c.Eval(expr)
	fields := map[string]string{"bytes": strconv.Itoa(len(html)), "phase": "started"}
	if err != nil {
		fields["phase"] = "failed"
		fields["error"] = debuglog.Error(err)
	}
	c.Logger.Log(BackendName, "load_html", fields)
	return err
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

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
