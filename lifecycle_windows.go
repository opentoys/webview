//go:build windows

package webview

import "github.com/opentoys/webview/internal/windows"

func newNativeTray(opts SystrayOptions) (trayBackend, error) { return windows.NewTray(opts) }
func stopNativeApplication() error                           { return windows.StopApplication() }

func runNativeApplication(start func() error, done <-chan struct{}) error {
	if err := start(); err != nil {
		return err
	}
	<-done
	windows.WaitApplication()
	return nil
}
