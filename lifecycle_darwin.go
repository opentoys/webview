//go:build darwin

package webview

import "github.com/opentoys/webview/internal/darwin"

func newNativeTray(opts SystrayOptions) (trayBackend, error) { return darwin.NewTray(opts) }
func stopNativeApplication() error                           { return darwin.StopApplication() }

func runNativeApplication(start func() error, done <-chan struct{}) error {
	err := darwin.RunApplication(func() error {
		err := start()
		if err != nil {
			_ = Quit()
		}
		return err
	})
	if err != nil {
		return err
	}
	<-done
	return nil
}
