package types

import (
	"net/url"
	"testing"
)

func TestLocalhostURL(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		want string
	}{
		{"path", "app://host/a/b", "https://app.localhost/a/b"},
		{"empty path", "app://host", "https://app.localhost/"},
		{"root", "app://host/", "https://app.localhost/"},
		{"query", "app://host/a?q=a%20b&x=%2F", "https://app.localhost/a?q=a%20b&x=%2F"},
		{"fragment", "app://host/a#section", "https://app.localhost/a#section"},
		{"escaped path and fragment", "app://host/a%2Fb%20c?q=%23#part%20one", "https://app.localhost/a/b c?q=%23#part one"},
		{"double slash", "app://host//a", "https://app.localhost//a"},
		{"empty query", "app://host?", "https://app.localhost/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := LocalhostURL("app", u); got != tt.want {
				t.Fatalf("LocalhostURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
