package chrome

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/opentoys/webview/internal/types"
)

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
