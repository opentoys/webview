//go:build !darwin && !linux && !windows

package webview

func newNativeTray(opts SystrayOptions) (trayBackend, error) { return nil, ErrMenuUnsupported }
func stopNativeApplication() error                           { return nil }

func runNativeApplication(start func() error, done <-chan struct{}) error {
	if err := start(); err != nil {
		return err
	}
	<-done
	return nil
}
