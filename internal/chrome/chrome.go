// Package chrome implements a Chrome/Chromium webview backend driven through
// the Chrome DevTools Protocol over --remote-debugging-pipe.
package chrome

import (
	"encoding/json"
	"os"
	"os/exec"
	"sync"

	"github.com/opentoys/webview/internal/debuglog"
	"github.com/opentoys/webview/internal/types"
)

const (
	BackendName string = "chrome"
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
	Offscreen bool
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
	// targetID identifies the app window's page target. Chrome also attaches
	// browser UI, workers, extensions, and other transient targets; their
	// lifecycle events must not close the app.
	targetID string

	schemeHandlers map[string]types.ResourceHandler
	tmpDir         string
	stderrLog      *os.File

	Debug      bool
	Logger     *debuglog.Logger
	Incognito  bool
	DataDir    string
	Offscreen  bool
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
		Offscreen:  opts.Offscreen,
		Executable: opts.Executable,
	}
}
