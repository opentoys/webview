//go:build windows

package windows

// Custom scheme rewriting and WebView2 resource interception.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unsafe"
)

func (p *Platform) rewriteSchemeURL(rawURL string) string {
	for scheme := range p.schemeHandlers {
		if strings.HasPrefix(rawURL, scheme+"://") {
			u, err := url.Parse(rawURL)
			if err != nil {
				return rawURL
			}
			out := fmt.Sprintf("https://%s.localhost/%s", scheme, strings.TrimPrefix(u.Path, "/"))
			if u.RawQuery != "" {
				out += "?" + u.RawQuery
			}
			if u.Fragment != "" {
				out += "#" + u.Fragment
			}
			return out
		}
	}
	return rawURL
}

// InvokeWebResourceRequested handles WebResourceRequested events from WebView2.
// Extracts the scheme from the URL, looks up the handler, and dispatches.
func (p *Platform) InvokeWebResourceRequested(sender *iCoreWebView2, args *iCoreWebView2WebResourceRequestedEventArgs) uintptr {
	req := args.GetRequest()
	if req == nil {
		return 0
	}

	uri := req.GetUri()
	if uri == "" {
		return 0
	}

	// Parse scheme from URL: https://app.localhost/path → "app"
	scheme := ""
	u, err := url.Parse(uri)
	if err == nil {
		host := u.Hostname()
		if idx := strings.Index(host, "."); idx > 0 {
			scheme = host[:idx]
		}
	}
	handler, ok := p.schemeHandlers[scheme]
	if !ok {
		return 0
	}

	method := req.GetMethod()
	if method == "" {
		method = http.MethodGet
	}
	headers := req.GetHeaders()

	sr := ResourceRequest{
		URL:     uri,
		Method:  method,
		Headers: headers,
	}
	switch method {
	case "GET", "HEAD", "TRACE", "OPTIONS":
	default:
		sr.Body = req.GetContent().ReadAll()
	}

	deferral := args.GetRequestDeferral()

	var gotResponse bool
	var syncResp *ResourceResponse
	handler(sr, func(resp *ResourceResponse) {
		if gotResponse {
			// Async response: dispatch to UI thread.
			p.dispatch.push(func() {
				p.applyResponse(args, deferral, resp)
			})
			pPostMessageW.Call(p.hwnd, WM_APP, 0, 0)
			return
		}
		// Synchronous response: store and apply after handler returns.
		gotResponse = true
		syncResp = resp
	})

	if gotResponse {
		p.applyResponse(args, deferral, syncResp)
	} else {
		deferral.Complete()
	}

	return 0
}

// applyResponse delivers the resource response to the webview via
// PutResponse + deferral. Called on the UI thread.
func (p *Platform) applyResponse(args *iCoreWebView2WebResourceRequestedEventArgs, deferral *iCoreWebView2Deferral, resp *ResourceResponse) {
	if resp == nil || len(resp.Body) == 0 {
		deferral.Complete()
		return
	}

	stream := createStreamOnHGlobal(resp.Body)
	if stream == nil {
		deferral.Complete()
		return
	}
	defer stream.vtbl.Release.Call(uintptr(unsafe.Pointer(stream)))

	webResp := p.env.CreateWebResourceResponse(
		"OK", resp.StatusCode, "", stream,
	)
	defer webResp.vtbl.Release.Call(uintptr(unsafe.Pointer(webResp)))

	if len(resp.Headers) > 0 {
		var parts []string
		for k, v := range resp.Headers {
			parts = append(parts, k+": "+strings.Join(v, ";"))
		}
		webResp.PutHeaders(strings.Join(parts, "\n"))
	}

	args.PutResponse(webResp)
	deferral.Complete()
}

// EvalHost evaluates JS from any goroutine by dispatching to the COM thread.
func (p *Platform) EvalHost(js string) {
	if p.ready.Load() == 0 {
		return
	}
	p.dispatch.push(func() {
		if p.webview != nil {
			p.webview.ExecuteScript(js, 0)
		}
	})
	pPostMessageW.Call(p.hwnd, WM_APP, 0, 0)
}

// InterceptResource registers a resource handler for the given URL scheme.
// Must be called before Run(). scheme is the URL scheme without "://"
// (e.g. "app"). On Windows, URLs like app://path are rewritten to
// https://app.localhost/path (secure context) and intercepted via
// WebResourceRequested.
func (p *Platform) InterceptResource(scheme string, handler ResourceHandler) {
	p.schemeHandlers[scheme] = handler
}
