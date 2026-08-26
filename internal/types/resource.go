package types

import "net/http"

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

type ResourceHandler func(req ResourceRequest, respond func(*ResourceResponse))
