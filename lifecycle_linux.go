//go:build linux

package webview

import "github.com/opentoys/webview/internal/linux"

func newNativeTray(opts SystrayOptions) (trayBackend, error) { return linux.NewTray(opts) }
func stopNativeApplication() error                           { return linux.StopApplication() }

func runNativeApplication(start func() error, done <-chan struct{}) error {
	if err := start(); err != nil {
		return err
	}
	<-done
	linux.WaitApplication()
	return nil
}
