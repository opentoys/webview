package chrome

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
)

var errClosed = errors.New("webview/chrome: connection closed")

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
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(m.Params, &p) == nil && p.TargetInfo.Type == "page" {
			// The first page is the --app window. Do not replace it when Chrome
			// later auto-attaches a popup or another page target.
			c.once.Do(func() {
				c.Lock()
				c.sessionID = p.SessionID
				c.targetID = p.TargetInfo.TargetID
				c.Unlock()
				close(c.attached)
			})
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
		if c.isMainTargetClosed(m.Method, m.Params) {
			// The app window was closed. Terminate the remaining browser process
			// so Run returns once cmd.Wait unblocks.
			go c.Close()
		}
	}
}

// isMainTargetClosed reports whether a Target lifecycle event belongs to the
// page selected as the app window. Events for browser UI, workers, extensions,
// and other transient targets are intentionally ignored.
func (c *Chrome) isMainTargetClosed(method string, params json.RawMessage) bool {
	var p struct {
		SessionID string `json:"sessionId"`
		TargetID  string `json:"targetId"`
	}
	if json.Unmarshal(params, &p) != nil {
		return false
	}
	c.Lock()
	sessionID, targetID := c.sessionID, c.targetID
	c.Unlock()

	switch method {
	case "Target.detachedFromTarget":
		return sessionID != "" && p.SessionID == sessionID ||
			targetID != "" && p.TargetID == targetID
	case "Target.targetDestroyed":
		return targetID != "" && p.TargetID == targetID
	default:
		return false
	}
}
