package types

type ResourceRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
}

type ResourceResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

type ResourceHandler func(req ResourceRequest, respond func(*ResourceResponse))
