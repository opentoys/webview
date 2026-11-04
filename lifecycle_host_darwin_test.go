//go:build darwin

package webview

import (
	"fmt"
	"os"
	"testing"

	"github.com/opentoys/webview/internal/darwin"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("WEBVIEW_DIALOG_HOST_PROBE"); mode != "" {
		if err := dialogHostProbe(mode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	code := 1
	finished := make(chan struct{})
	err := darwin.RunApplication(func() error {
		go func() {
			code = m.Run()
			close(finished)
			_ = darwin.StopApplication()
		}()
		return nil
	})
	if err != nil {
		panic(err)
	}
	<-finished
	os.Exit(code)
}
