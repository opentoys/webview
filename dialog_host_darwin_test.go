//go:build darwin

package webview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/opentoys/webview/internal/darwin"
)

// Invoked by TestMain before its shared AppKit host can mask initialization bugs.
func dialogHostProbe(mode string) error {
	if mode == "thread" {
		result := make(chan error, 1)
		go func() {
			_, err := ChooseDir()
			result <- err
		}()
		if err := <-result; !errors.Is(err, ErrDialogMainThread) {
			return fmt.Errorf("wrong thread: %v", err)
		}
		return nil
	}
	choose := ChooseDir
	switch mode {
	case "open":
		choose = func() (string, error) { return ChooseFile(FileFilter{Name: "Text", Extensions: []string{"txt"}}) }
	case "save":
		choose = func() (string, error) {
			return SaveFile(FileDialogOptions{
				Title:     "Standalone save verification",
				Directory: os.TempDir(),
				Filename:  "webview-dialog-probe.txt",
			})
		}
	}
	cancelDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if mode != "quit" {
					darwin.CancelDialog()
				}
			case <-cancelDone:
				return
			}
		}
	}()
	defer close(cancelDone)
	if mode == "quit" {
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = Quit()
		}()
		if _, err := choose(); !errors.Is(err, ErrApplicationStopped) {
			return fmt.Errorf("quit: %v", err)
		}
		return nil
	}
	if _, err := choose(); !errors.Is(err, ErrDialogCancelled) {
		return fmt.Errorf("before Run: %v", err)
	}
	ready := make(chan struct{})
	app.runNative = func(start func() error, done <-chan struct{}) error {
		return runNativeApplication(func() error {
			err := start()
			close(ready)
			return err
		}, done)
	}
	result := make(chan error, 1)
	go func() {
		<-ready
		_, err := choose()
		if !errors.Is(err, ErrDialogCancelled) {
			result <- fmt.Errorf("during Run: %v", err)
		} else {
			result <- nil
		}
		_ = Quit()
	}()
	if err := Run(); err != nil {
		return err
	}
	return <-result
}

func TestDialogFreshHost(t *testing.T) {
	for _, mode := range []string{"thread", "open", "directory", "save", "quit"} {
		t.Run(mode, func(t *testing.T) {
			if mode != "thread" && os.Getenv("WEBVIEW_NATIVE_DIALOG_TESTS") != "1" {
				t.Skip("set WEBVIEW_NATIVE_DIALOG_TESTS=1 to exercise native panels")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "WEBVIEW_DIALOG_HOST_PROBE="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, output)
			}
		})
	}
}
