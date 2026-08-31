package chrome

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/opentoys/webview/internal/debuglog"
)

// defaultChromeArgs are the stability flags shared by all Chrome invocations.
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
// Run launches Chrome and blocks until the window is closed.
func (c *Chrome) Run() error {
	c.Logger.Log(BackendName, "run_start", nil)
	if err := c.start(); err != nil {
		c.Logger.Log(BackendName, "error", map[string]string{"operation": "start", "error": debuglog.Error(err)})
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
	c.Logger.Log(BackendName, "ready", nil)

	err := c.cmd.Wait()
	c.Logger.Log(BackendName, "closed", nil)
	return err
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
	c.Logger.Log(BackendName, "close_requested", nil)
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
