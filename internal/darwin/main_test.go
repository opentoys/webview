//go:build darwin

package darwin

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := 1
	finished := make(chan struct{})
	if err := RunApplication(func() error {
		go func() {
			code = m.Run()
			close(finished)
			_ = StopApplication()
		}()
		return nil
	}); err != nil {
		panic(err)
	}
	<-finished
	os.Exit(code)
}
