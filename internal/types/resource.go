package types

import (
	"net/http"
	"strconv"
	"strings"
)

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

// MayHaveRequestBody reports whether a resource request is worth reading from
// the platform body stream. It avoids opening empty streams for common
// navigations while preserving POST/PUT/PATCH bodies whose length is unknown
// (for example, a streamed Blob or multipart FormData upload).
func MayHaveRequestBody(method string, headers http.Header) bool {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "TRACE":
		return false
	}

	// WebKit may advertise Content-Length: 0 for a Blob whose bytes are only
	// available through HTTPBodyStream. POST/PUT/PATCH must therefore remain
	// conservative regardless of the headers.
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH":
		return true
	}

	if length := headers.Get("Content-Length"); length != "" {
		if n, err := strconv.ParseInt(length, 10, 64); err == nil {
			return n > 0
		}
	}
	if headers.Get("Transfer-Encoding") != "" || headers.Get("Content-Type") != "" {
		return true
	}

	return false
}
