package debuglog

import (
	"bytes"
	"io"
	"testing"
)

func TestWriteStableAndEscaped(t *testing.T) {
	var b bytes.Buffer
	New(&b).Log("chrome", "ready", map[string]string{"z": "last", "a": "two words", "event": "ignored"})
	want := "{\"a\":\"two words\",\"backend\":\"chrome\",\"event\":\"ready\",\"z\":\"last\"}\n"
	if b.String() != want {
		t.Fatalf("log = %q, want %q", b.String(), want)
	}
}

func TestURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:secret@example.com/a?q=private#fragment": "https://example.com/a",
		"app://host/index.html?token=x":                        "app://host/index.html",
		"data:text/html;base64,SGVsbG8=":                       "data:text/html;bytes=8",
		"file:///Users/name/secret.html":                       "file:local",
	} {
		if got := URL(raw); got != want {
			t.Errorf("URL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDisabledDoesNotWrite(t *testing.T) {
	var b bytes.Buffer
	New(io.Discard).Log("chrome", "ready", nil)
	if b.Len() != 0 {
		t.Fatalf("disabled log wrote %q", b.String())
	}
}
