package types

// ResourceRequest matches the webview package's ResourceRequest.
type ResourceRequest struct {
	URL     string
	Method  string
	Headers map[string]string
}

// ResourceResponse matches the webview package's ResourceResponse.
type ResourceResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// ResourceHandler matches the webview package's ResourceHandler.
type ResourceHandler func(req ResourceRequest, respond func(*ResourceResponse))
