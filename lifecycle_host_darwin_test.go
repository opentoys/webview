//go:build darwin

package webview

import (
	"os"
	"testing"

	"github.com/opentoys/webview/internal/darwin"
)

func TestMain(m *testing.M) {
	code := 1
	finished := make(chan struct{})
	err := darwin.RunApplication(func() error {
		go func() { code = m.Run(); close(finished); _ = darwin.StopApplication() }()
		return nil
	})
	if err != nil {
		panic(err)
	}
	<-finished
	os.Exit(code)
}
