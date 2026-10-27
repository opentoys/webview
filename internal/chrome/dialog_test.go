package chrome

import (
	"errors"
	"testing"

	"github.com/opentoys/webview/internal/types"
)

func TestNativeDialogsUnsupported(t *testing.T) {
	c := New(Options{})
	for name, choose := range map[string]func() (string, error){
		"file":      func() (string, error) { return c.ChooseFile() },
		"directory": c.ChooseDir,
	} {
		t.Run(name, func(t *testing.T) {
			path, err := choose()
			if path != "" || !errors.Is(err, types.ErrDialogUnsupported) {
				t.Fatalf("choose() = %q, %v", path, err)
			}
		})
	}
}
