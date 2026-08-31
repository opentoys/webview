// Package debuglog contains the small, deliberately non-sensitive debug log
// format shared by every webview backend.
package debuglog

import (
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Logger writes serialized debug events to an io.Writer.
type Logger struct {
	w  io.Writer
	mu sync.Mutex
}

// New returns a Logger that writes to w. A nil writer is treated as io.Discard.
func New(w io.Writer) *Logger {
	if w == nil {
		w = io.Discard
	}
	return &Logger{w: w}
}

// Log writes one stable JSON diagnostic object on a single line.
func (l *Logger) Log(backend, event string, fields map[string]string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record := make(map[string]string, len(fields)+2)
	record["backend"] = backend
	record["event"] = event
	for key, value := range fields {
		if key != "backend" && key != "event" {
			record[key] = value
		}
	}
	b, err := json.Marshal(record)
	if err == nil {
		_, _ = l.w.Write(append(b, '\n'))
	}
}

// URL removes credentials, queries and fragments. Local paths are never
// emitted; data URLs disclose only their MIME type and byte length.
func URL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "invalid"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "data" {
		meta, data, found := strings.Cut(raw[len(u.Scheme)+1:], ",")
		if !found {
			data = ""
		}
		mime := strings.Split(meta, ";")[0]
		if mime == "" {
			mime = "text/plain"
		}
		return "data:" + mime + ";bytes=" + strconv.Itoa(len(data))
	}
	if scheme == "file" {
		return "file:local"
	}
	// Opaque URLs (about:blank, mailto: etc.) carry no host/path to preserve.
	if u.Opaque != "" {
		return scheme + ":"
	}
	host := u.Hostname()
	if u.Port() != "" {
		host += ":" + u.Port()
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return scheme + "://" + host + path
}

// Error is a bounded error summary suitable for diagnostics without exposing
// payloads that a lower layer may have embedded in an error string.
func Error(err error) string {
	if err == nil {
		return ""
	}
	// Errors returned by browser protocols often echo request data or JavaScript.
	// Keep the public diagnostic deliberately generic rather than guessing which
	// portions of an arbitrary error string are safe to disclose.
	return "operation failed"
}
