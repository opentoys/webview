package types

import (
	"net/http"
	"testing"
)

func TestMayHaveRequestBody(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		headers http.Header
		want    bool
	}{
		{"GET", "GET", nil, false},
		{"explicit empty POST", "POST", http.Header{"Content-Length": {"0"}}, false},
		{"streamed blob", "POST", nil, true},
		{"multipart file", "PUT", http.Header{"Content-Type": {"multipart/form-data; boundary=x"}}, true},
		{"DELETE without body", "DELETE", nil, false},
		{"DELETE with body", "DELETE", http.Header{"Content-Length": {"3"}}, true},
		{"chunked PATCH", "PATCH", http.Header{"Transfer-Encoding": {"chunked"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MayHaveRequestBody(tt.method, tt.headers); got != tt.want {
				t.Fatalf("MayHaveRequestBody(%q, %v) = %v, want %v", tt.method, tt.headers, got, tt.want)
			}
		})
	}
}
